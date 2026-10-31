package agent

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests below never print a generated code or token: assertions
// check lengths, charset membership, file contents (hash only) and
// HTTP outcomes, never the secret values themselves.

func TestPairingCharset(t *testing.T) {
	if got := len(pairingCharset); got != 56 {
		t.Fatalf("pairingCharset length: got %d, want 56", got)
	}
	for _, bad := range []byte{'0', 'O', 'o', '1', 'l', 'I'} {
		if strings.ContainsRune(pairingCharset, rune(bad)) {
			t.Fatalf("pairingCharset contains lookalike %q", bad)
		}
	}
	seen := map[byte]bool{}
	for i := 0; i < len(pairingCharset); i++ {
		if seen[pairingCharset[i]] {
			t.Fatalf("pairingCharset duplicates %q", pairingCharset[i])
		}
		seen[pairingCharset[i]] = true
	}
}

func TestGeneratePairingCode(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := GeneratePairingCode()
		if err != nil {
			t.Fatalf("GeneratePairingCode: %v", err)
		}
		if len(code) != pairingCodeLength {
			t.Fatalf("code length: got %d, want %d", len(code), pairingCodeLength)
		}
		for j := 0; j < len(code); j++ {
			if !strings.ContainsRune(pairingCharset, rune(code[j])) {
				t.Fatalf("code contains char %q outside charset", code[j])
			}
		}
	}
}

func TestGenerateLongToken(t *testing.T) {
	tok, err := GenerateLongToken()
	if err != nil {
		t.Fatalf("GenerateLongToken: %v", err)
	}
	if len(tok) != 43 {
		t.Fatalf("token length: got %d, want 43", len(tok))
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("token is not RawURLEncoding: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("token decodes to %d bytes, want 32", len(raw))
	}
}

func testManager(t *testing.T, now time.Time) *PairingManager {
	t.Helper()
	m := NewPairingManager(t.TempDir())
	m.now = func() time.Time { return now }
	return m
}

func TestPairingFileStoresHashNotCode(t *testing.T) {
	dir := t.TempDir()
	m := NewPairingManager(dir)
	m.now = func() time.Time { return time.Unix(1000, 0) }
	code, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, pairingFileName))
	if err != nil {
		t.Fatalf("read pairing file: %v", err)
	}
	if strings.Contains(string(data), code) {
		t.Fatalf("pairing file contains the plaintext code")
	}
	var st pairingState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal pairing file: %v", err)
	}
	if st.CodeHash != hashPairingCode(code) {
		t.Fatalf("pairing file hash does not match the code hash")
	}
	if st.ExpiresAt != 1000+300 || st.GeneratedAt != 1000 || st.Failures != 0 {
		t.Fatalf("pairing state = %+v, want expiry 1300, generated 1000, failures 0", st)
	}
	if info, err := os.Stat(filepath.Join(dir, pairingFileName)); err != nil {
		t.Fatalf("stat pairing file: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("pairing file mode: got %o, want 600", info.Mode().Perm())
	}
}

func TestPairingOneTimeAndExpiry(t *testing.T) {
	now := time.Unix(2000, 0)
	m := testManager(t, now)
	code, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !m.Verify(code) {
		t.Fatalf("first Verify: got false, want true")
	}
	if m.Verify(code) {
		t.Fatalf("second Verify: got true, want false (one-time)")
	}

	// Expiry: a code generated at T is dead at T+300s.
	m2 := testManager(t, now)
	code2, err := m2.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	m2.now = func() time.Time { return now.Add(5 * time.Minute) }
	if m2.Verify(code2) {
		t.Fatalf("Verify at expiry: got true, want false")
	}
	if _, err := os.Stat(filepath.Join(m2.dir, pairingFileName)); !os.IsNotExist(err) {
		t.Fatalf("expired pairing file was not removed")
	}
}

func TestPairingFiveFailuresInvalidate(t *testing.T) {
	now := time.Unix(3000, 0)
	m := testManager(t, now)
	code, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Four wrong attempts: the code still works.
	for i := 0; i < 4; i++ {
		if m.Verify("ZZZZZZZZ") {
			t.Fatalf("wrong Verify %d: got true, want false", i+1)
		}
	}
	if !m.Verify(code) {
		t.Fatalf("Verify after 4 failures: got false, want true")
	}

	// Five wrong attempts on a fresh code: the code is void, and
	// the correct code no longer works either.
	m2 := testManager(t, now)
	code2, err := m2.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for i := 0; i < 5; i++ {
		if m2.Verify("ZZZZZZZZ") {
			t.Fatalf("wrong Verify %d: got true, want false", i+1)
		}
	}
	if m2.Verify(code2) {
		t.Fatalf("Verify after 5 failures: got true, want false")
	}
	if _, err := os.Stat(filepath.Join(m2.dir, pairingFileName)); !os.IsNotExist(err) {
		t.Fatalf("pairing file was not removed after 5 failures")
	}
}

func TestPairingRequestInterval(t *testing.T) {
	now := time.Unix(4000, 0)
	m := testManager(t, now)
	first, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// 30s later: refused, pending code untouched.
	m.now = func() time.Time { return now.Add(30 * time.Second) }
	if code, retry, err := m.Request(); err != nil || code != "" || retry != 30 {
		t.Fatalf("Request at +30s: got code-len %d retry %d err %v, want empty/30/nil", len(code), retry, err)
	}
	if !m.Verify(first) {
		t.Fatalf("pending code after refused request: got false, want true")
	}
	// 61s after generation: a fresh code replaces the old one.
	m2 := testManager(t, now)
	oldCode, err := m2.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	m2.now = func() time.Time { return now.Add(61 * time.Second) }
	if code, retry, err := m2.Request(); err != nil || code == "" || retry != 0 {
		t.Fatalf("Request at +61s: got code-len %d retry %d err %v, want fresh/0/nil", len(code), retry, err)
	} else if m2.Verify(oldCode) {
		t.Fatalf("old code still verifies after regeneration, want invalid")
	}
}

func TestLoadTokenPrecedence(t *testing.T) {
	dir := t.TempDir()
	if tok, err := LoadToken(dir, "env-token"); err != nil || tok != "env-token" {
		t.Fatalf("LoadToken env fallback: got %q err %v", tok, err)
	}
	if err := WriteTokenFile(dir, "file-token"); err != nil {
		t.Fatalf("WriteTokenFile: %v", err)
	}
	if tok, err := LoadToken(dir, "env-token"); err != nil || tok != "file-token" {
		t.Fatalf("LoadToken file precedence: got %q err %v", tok, err)
	}
	if info, err := os.Stat(filepath.Join(dir, tokenFileName)); err != nil {
		t.Fatalf("stat token file: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode: got %o, want 600", info.Mode().Perm())
	}
}

func TestSyncEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.env")
	orig := "# comment\nSINGBOX_AGENT_ADDR=:32435\nSINGBOX_AGENT_TOKEN=old-value\nOTHER=keep\n"
	if err := os.WriteFile(path, []byte(orig), 0o640); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if err := SyncEnvFile(path, "new-value"); err != nil {
		t.Fatalf("SyncEnvFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "SINGBOX_AGENT_TOKEN=new-value\n") {
		t.Fatalf("env file token line not updated: %q", got)
	}
	if !strings.Contains(got, "# comment\n") || !strings.Contains(got, "OTHER=keep\n") {
		t.Fatalf("env file other lines not preserved: %q", got)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat env file: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("env file mode: got %o, want 640 (preserved)", info.Mode().Perm())
	}
	// Appends the line when missing.
	path2 := filepath.Join(dir, "fresh.env")
	if err := SyncEnvFile(path2, "tok-1"); err != nil {
		t.Fatalf("SyncEnvFile missing file: %v", err)
	}
	if data, err := os.ReadFile(path2); err != nil {
		t.Fatalf("read fresh env: %v", err)
	} else if string(data) != "SINGBOX_AGENT_TOKEN=tok-1\n" {
		t.Fatalf("fresh env content: got %q", string(data))
	}
}

// TestServerPairingRotation exercises the HTTP pair endpoint end to
// end: the old token works before pairing, fails after a successful
// pairing, and the new token works; a failed pairing leaves the old
// token working. Token values are never logged or printed here.
func TestServerPairingRotation(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "agent.env")
	if err := os.WriteFile(envPath, []byte("SINGBOX_AGENT_ADDR=:32435\nSINGBOX_AGENT_TOKEN=old-token\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	ag, err := New(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}
	srv := NewServer(ag, "old-token", ":0", "", "", dir, true, nil)
	srv.SetEnvFile(envPath)

	authOK := func(token string) bool {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/agent/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		called := false
		srv.auth(func(w http.ResponseWriter, r *http.Request) { called = true })(rec, req)
		return called
	}
	if !authOK("old-token") {
		t.Fatalf("old token should authenticate before pairing")
	}

	// Failed pairing: old token still works.
	mgr := srv.Pairing()
	if _, err := mgr.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/pair", strings.NewReader(`{"code":"ZZZZZZZZ"}`))
	srv.handlePair(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("failed pair status: got %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "配对码无效或已过期") {
		t.Fatalf("failed pair body: got %q, want the generic message", body)
	}
	if !authOK("old-token") {
		t.Fatalf("old token should still authenticate after a failed pairing")
	}

	// Successful pairing: token rotates in memory, file and env.
	// The code is read back through the manager's own state only
	// via a fresh generation (the file holds a hash, never the
	// code), so generate a code here and pair with it directly.
	code, err := mgr.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agent/pair", strings.NewReader(`{"code":"`+code+`"}`))
	srv.handlePair(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("successful pair status: got %d, body %q", rec.Code, rec.Body.String())
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal pair response: %v", err)
	}
	if len(res.Token) != 43 {
		t.Fatalf("returned token length: got %d, want 43", len(res.Token))
	}
	if authOK("old-token") {
		t.Fatalf("old token should fail after a successful pairing")
	}
	if !authOK(res.Token) {
		t.Fatalf("new token should authenticate after pairing")
	}
	if fileTok, err := ReadTokenFile(dir); err != nil || fileTok != res.Token {
		t.Fatalf("token file mismatch after pairing (err %v)", err)
	}
	if envData, err := os.ReadFile(envPath); err != nil {
		t.Fatalf("read env: %v", err)
	} else if !strings.Contains(string(envData), "SINGBOX_AGENT_TOKEN="+res.Token) {
		t.Fatalf("env file was not synced to the new token")
	}
	if _, err := os.Stat(filepath.Join(dir, pairingFileName)); !os.IsNotExist(err) {
		t.Fatalf("pairing file was not removed after success")
	}
}

func TestServerRequestPairingCodeNeverReturnsCode(t *testing.T) {
	dir := t.TempDir()
	ag, err := New(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("New agent: %v", err)
	}
	srv := NewServer(ag, "tok", ":0", "", "", dir, true, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/request-pairing-code", strings.NewReader(`{}`))
	srv.handleRequestPairingCode(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request status: got %d", rec.Code)
	}
	var res struct {
		OK         bool `json:"ok"`
		RetryAfter int  `json:"retry_after"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal request response: %v", err)
	}
	if !res.OK || res.RetryAfter != 0 {
		t.Fatalf("request response: got %+v, want ok=true retry_after=0", res)
	}
	// The response must not contain a code field at all.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw response: %v", err)
	}
	if _, ok := raw["code"]; ok {
		t.Fatalf("request response contains a code field")
	}
	// Immediate second request is refused with retry_after and does
	// not invalidate the pending code.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/agent/request-pairing-code", strings.NewReader(`{}`))
	srv.handleRequestPairingCode(rec2, req2)
	var res2 struct {
		OK         bool `json:"ok"`
		RetryAfter int  `json:"retry_after"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("unmarshal second response: %v", err)
	}
	if res2.OK || res2.RetryAfter <= 0 || res2.RetryAfter > 60 {
		t.Fatalf("second request: got %+v, want ok=false retry_after in (0,60]", res2)
	}
}
