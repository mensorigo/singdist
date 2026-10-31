package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pairing is the agent-side pairing flow. The panel never generates
// the long-term token: the agent does, and only after the operator
// proves possession of the node by entering a short pairing code
// that was printed on the agent's own terminal/log.
//
// Security shape (do not weaken it):
//   - The pairing code is 8 characters from an unambiguous set,
//     valid 5 minutes, one-time, case-sensitive.
//   - pairing.json stores ONLY the code's SHA-256 hash, the expiry,
//     the failure count and the generation time — never the code
//     itself. A restarted agent cannot re-print an old code (it only
//     has the hash); a fresh code must be generated instead.
//   - The long-term token is never printed and never logged. It is
//     returned to the panel exactly once, inside the successful
//     /api/agent/pair response.
//   - The old token keeps working until a pairing succeeds. Only
//     then is it replaced (file, env file and memory together);
//     a failed or expired pairing leaves the old token untouched.
const (
	pairingCodeLength   = 8
	pairingValidity     = 5 * time.Minute
	pairingMaxFailures  = 5
	pairingRequestDelay = 60 * time.Second
	pairingFileName     = "pairing.json"
	tokenFileName       = "agent-token"
)

// pairingCharset is 56 characters: digits 2-9, uppercase without I
// and O, lowercase without l and o. The lookalikes 0/O/o, 1/l/I
// are all absent, so a code read off a terminal cannot be confused.
const pairingCharset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"

func hashPairingCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// GeneratePairingCode draws 8 characters from pairingCharset with
// crypto/rand.
func GeneratePairingCode() (string, error) {
	var b strings.Builder
	b.Grow(pairingCodeLength)
	max := big.NewInt(int64(len(pairingCharset)))
	for i := 0; i < pairingCodeLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("pairing code randomness: %w", err)
		}
		b.WriteByte(pairingCharset[n.Int64()])
	}
	return b.String(), nil
}

// GenerateLongToken draws 32 random bytes and encodes them as
// base64 RawURLEncoding (43 characters, 256 bits of entropy).
func GenerateLongToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("long token randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

type pairingState struct {
	CodeHash    string `json:"codeHash"`
	ExpiresAt   int64  `json:"expiresAt"`
	Failures    int    `json:"failures"`
	GeneratedAt int64  `json:"generatedAt"`
}

// PairingManager owns pairing.json. It keeps no state in memory:
// every operation loads the file, so a CLI invocation, a server
// restart and the running server all see the same pending code.
type PairingManager struct {
	dir string
	mu  sync.Mutex
	now func() time.Time // nil = time.Now; tests set a fixed clock
}

func NewPairingManager(dir string) *PairingManager {
	return &PairingManager{dir: dir}
}

func (m *PairingManager) nowTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *PairingManager) path() string { return filepath.Join(m.dir, pairingFileName) }

func (m *PairingManager) load() (*pairingState, error) {
	data, err := os.ReadFile(m.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st pairingState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (m *PairingManager) save(st *pairingState) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeFileAtomic(m.path(), data, 0o600)
}

func (m *PairingManager) remove() {
	_ = os.Remove(m.path())
}

// writeFileAtomic writes data to a temp file in the same directory
// (mode perm) and renames it over path, so a crash mid-write never
// leaves a half-written credential file behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Generate creates a fresh pairing code, invalidating any pending
// code, and returns it for the caller to print. It deliberately
// bypasses the HTTP 60-second interval: it is the operator's own
// action (CLI, or a no-token startup), not a remote request.
func (m *PairingManager) Generate() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	code, err := GeneratePairingCode()
	if err != nil {
		return "", err
	}
	now := m.nowTime().Unix()
	if err := m.save(&pairingState{
		CodeHash:    hashPairingCode(code),
		ExpiresAt:   now + int64(pairingValidity/time.Second),
		Failures:    0,
		GeneratedAt: now,
	}); err != nil {
		return "", err
	}
	return code, nil
}

// Request is the HTTP-triggered generation. It enforces the
// 60-second interval: when a code was generated less than 60 seconds
// ago, it returns ("", retryAfter, nil) without regenerating and
// without invalidating the pending code. Otherwise it generates a
// fresh code (invalidating the old pending one, if any) and returns
// it for the caller to print to the agent log — the code is never
// returned to the HTTP caller.
func (m *PairingManager) Request() (code string, retryAfter int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.nowTime().Unix()
	if st, lerr := m.load(); lerr == nil && st != nil {
		if elapsed := now - st.GeneratedAt; elapsed < int64(pairingRequestDelay/time.Second) {
			return "", int(int64(pairingRequestDelay/time.Second) - elapsed), nil
		}
	}
	// A corrupt file is overwritten by the fresh generation below;
	// a load error must not block a legitimate re-pair request.
	code, err = GeneratePairingCode()
	if err != nil {
		return "", 0, err
	}
	if err := m.save(&pairingState{
		CodeHash:    hashPairingCode(code),
		ExpiresAt:   now + int64(pairingValidity/time.Second),
		Failures:    0,
		GeneratedAt: now,
	}); err != nil {
		return "", 0, err
	}
	return code, 0, nil
}

// Verify checks a code against the pending hash. It is one-time
// (a success deletes the file) and failure-counted: the 5th failed
// attempt invalidates the code. Every failure, including an expired
// or absent code, is reported identically by the caller — Verify
// itself only returns true/false.
func (m *PairingManager) Verify(code string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if code == "" {
		return false
	}
	st, err := m.load()
	if err != nil || st == nil {
		return false
	}
	now := m.nowTime().Unix()
	if now >= st.ExpiresAt || st.Failures >= pairingMaxFailures {
		m.remove()
		return false
	}
	want, derr := hex.DecodeString(st.CodeHash)
	if derr != nil {
		m.remove()
		return false
	}
	got := sha256.Sum256([]byte(code))
	match := len(want) == len(got) && subtleCompare(want, got[:])
	if !match {
		st.Failures++
		if st.Failures >= pairingMaxFailures {
			m.remove()
		} else {
			_ = m.save(st)
		}
		return false
	}
	m.remove()
	return true
}

func subtleCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// --- long-term token persistence -----------------------------------------

// ReadTokenFile returns the token stored in <dir>/agent-token, or ""
// when the file does not exist. A present-but-empty file also yields
// "" (the caller then falls back to the environment token).
func ReadTokenFile(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, tokenFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteTokenFile atomically stores token in <dir>/agent-token (0600).
func WriteTokenFile(dir, token string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, tokenFileName), []byte(token), 0o600)
}

// RemoveTokenFile deletes <dir>/agent-token (used to roll a rotation
// back to the unpaired state when there was no previous token).
func RemoveTokenFile(dir string) {
	_ = os.Remove(filepath.Join(dir, tokenFileName))
}

// LoadToken resolves the startup token: the token file wins over the
// environment variable; both absent yields "" (unpaired mode).
func LoadToken(dir, envToken string) (string, error) {
	if fileToken, err := ReadTokenFile(dir); err != nil {
		return "", err
	} else if fileToken != "" {
		return fileToken, nil
	}
	return strings.TrimSpace(envToken), nil
}

// SyncEnvFile rewrites the SINGBOX_AGENT_TOKEN= line in the env file
// (creating the file 0600 when it does not exist), preserving every
// other line and the file's existing permissions. A missing token
// line is appended.
func SyncEnvFile(path, token string) error {
	var mode os.FileMode = 0o600
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		if info, serr := os.Stat(path); serr == nil {
			mode = info.Mode().Perm()
		}
		text := strings.TrimSuffix(string(data), "\n")
		if text != "" {
			lines = strings.Split(text, "\n")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		prefix := ""
		rest := trimmed
		if strings.HasPrefix(rest, "export ") {
			prefix = "export "
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "export "))
		}
		if strings.HasPrefix(rest, "SINGBOX_AGENT_TOKEN=") {
			lines[i] = prefix + "SINGBOX_AGENT_TOKEN=" + token
			found = true
		}
	}
	if !found {
		lines = append(lines, "SINGBOX_AGENT_TOKEN="+token)
	}
	out := strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(out), mode)
}
