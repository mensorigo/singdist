package coresync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplySetsConfigMode0644(t *testing.T) {
	dir := t.TempDir()
	// bin /bin/true stands in for `sing-box check`: it exits 0
	// whatever its arguments. unit "" skips the restart.
	s := &Service{target: filepath.Join(dir, "config.json"), bin: "/bin/true", unit: ""}
	if aerr := s.apply(context.Background(), []byte(`{"inbounds":[]}`), nil); aerr != nil {
		t.Fatalf("apply must succeed: %v", aerr)
	}
	// Regression: os.CreateTemp makes the temp file 0600, and the
	// core runs as the unprivileged sing-box user. A 0600
	// root-owned config makes the core fail with "read config:
	// permission denied" and restart-loop (seen on 2号机, 170+
	// restarts). The applied config must be readable by the core.
	fi, err := os.Stat(s.target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("applied config mode = %o, want 0644", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(s.target)
	if string(b) != `{"inbounds":[]}` {
		t.Fatalf("applied config content wrong, got %s", b)
	}
}

func TestWriteAtomicSetsConfigMode0644(t *testing.T) {
	dir := t.TempDir()
	s := &Service{target: filepath.Join(dir, "config.json")}
	if err := s.writeAtomic([]byte(`{"old":true}`)); err != nil {
		t.Fatalf("writeAtomic must succeed: %v", err)
	}
	fi, err := os.Stat(s.target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("writeAtomic config mode = %o, want 0644", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(s.target)
	if string(b) != `{"old":true}` {
		t.Fatalf("writeAtomic config content wrong, got %s", b)
	}
}
