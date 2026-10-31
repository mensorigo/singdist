package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// CoreInbounds is the import source: the panel reads the node's
// current core config through it. These tests pin the contract: raw
// objects verbatim (including unsupported types and secrets, which
// the panel filters/maps), empty list for a config without inbounds,
// and errors — never a silent empty list — for a missing or broken
// target file.
func TestCoreInboundsVerbatim(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	doc := `{"log": {"level": "info"}, "inbounds": [
		{"type": "hysteria2", "tag": "legacy-hy2", "listen_port": 18360,
		 "users": [{"name": "u1", "password": "pw-123"}],
		 "obfs": {"type": "salamander", "password": "obfs-123"},
		 "tls": {"enabled": true, "server_name": "node.example.com"}},
		{"type": "vmess", "tag": "old-vmess", "listen_port": 18361},
		{"type": "trojan", "tag": "legacy-tj", "listen_port": 18362,
		 "users": [{"name": "u1", "password": "tj-pw"}]}
	], "outbounds": [{"type": "direct", "tag": "direct"}]}`
	if err := os.WriteFile(target, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{DataDir: t.TempDir(), Target: target})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.CoreInbounds()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 inbounds, got %d", len(got))
	}
	// Verbatim: the hy2 object keeps its obfs block and user password.
	raw, _ := json.Marshal(got[0])
	var hy2 map[string]any
	if err := json.Unmarshal(raw, &hy2); err != nil {
		t.Fatal(err)
	}
	if hy2["tag"] != "legacy-hy2" || hy2["listen_port"] != float64(18360) {
		t.Errorf("hy2 identity lost: %v", hy2)
	}
	obfs, _ := hy2["obfs"].(map[string]any)
	if obfs["type"] != "salamander" || obfs["password"] != "obfs-123" {
		t.Errorf("hy2 obfs lost: %v", hy2["obfs"])
	}
	users, _ := hy2["users"].([]any)
	if len(users) != 1 || users[0].(map[string]any)["password"] != "pw-123" {
		t.Errorf("hy2 users lost: %v", hy2["users"])
	}
	// Unsupported types are returned too: filtering is the panel's job.
	if got[1]["type"] != "vmess" {
		t.Errorf("unsupported type must still be returned, got %v", got[1]["type"])
	}
}

func TestCoreInboundsEmptyAndErrors(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(`{"inbounds": [], "outbounds": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _ := New(Config{DataDir: t.TempDir(), Target: target})
	got, err := a.CoreInbounds()
	if err != nil || len(got) != 0 {
		t.Errorf("empty inbounds: got %v, err %v", got, err)
	}

	missing, _ := New(Config{DataDir: t.TempDir(), Target: filepath.Join(dir, "nope.json")})
	if _, err := missing.CoreInbounds(); err == nil {
		t.Error("missing target file must error, not return an empty list")
	}

	broken := filepath.Join(dir, "broken.json")
	_ = os.WriteFile(broken, []byte(`{not json`), 0o600)
	bad, _ := New(Config{DataDir: t.TempDir(), Target: broken})
	if _, err := bad.CoreInbounds(); err == nil {
		t.Error("unparseable target file must error, not return an empty list")
	}
}
