package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the agent's durable state, kept in a single JSON file
// (0600: the last doc carries credentials). It replaces the panel's
// sync_state row: the last applied doc is the baseline for
// passthrough decisions and the rollback target, and the blocked
// hash stops a failed input from retrying in a loop.
type State struct {
	LastDoc     string `json:"lastDoc"`
	LastHash    string `json:"lastHash"`
	BlockedHash string `json:"blockedHash"`
	LastSyncAt  int64  `json:"lastSyncAt"`
	LastResult  string `json:"lastResult"`
	UpdatedAt   int64  `json:"updatedAt"`
}

func nowUnix() int64 { return time.Now().Unix() }

// loadState reads the state file; a missing file means "never
// synced" (first apply), not an error.
func loadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// saveState writes the state file atomically (temp + rename, 0600).
func saveState(path string, st *State) error {
	st.UpdatedAt = nowUnix()
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agent-state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LocalFile is the optional local inbounds file: inbounds the
// central panel does not manage, defined on the node itself. Its
// cert (if any) is the fallback certificate for local TLS inbounds
// when the push carries none.
type LocalFile struct {
	Inbounds []Inbound `json:"inbounds"`
	Cert     *Cert     `json:"cert,omitempty"`
}

// loadLocalFile reads the local inbounds file; a missing file (or
// an unconfigured path) means "no local inbounds".
func loadLocalFile(path string, reserved map[int]string) (*LocalFile, error) {
	if path == "" {
		return &LocalFile{}, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &LocalFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var lf LocalFile
	if err := json.Unmarshal(b, &lf); err != nil {
		return nil, err
	}
	validated, err := validateInbounds(lf.Inbounds, reserved)
	if err != nil {
		return nil, err
	}
	lf.Inbounds = validated
	return &lf, nil
}

// backupOriginal copies the pre-agent target file into the data dir
// (0600: it may carry credentials) so the first apply, which takes
// over the core config entirely, can be undone by hand.
// Best-effort: a backup failure never fails the apply.
func backupOriginal(dataDir string, fileBytes []byte) {
	if len(fileBytes) == 0 {
		return
	}
	path := filepath.Join(dataDir, "original-config-backup.json")
	_ = os.WriteFile(path, fileBytes, 0o600)
}
