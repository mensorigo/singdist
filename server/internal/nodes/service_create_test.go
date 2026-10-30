package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"singbox-ui/server/internal/store"
)

// statusAgent serves one fixed /api/agent/status body (hostname may be
// absent, modelling an agent older than 0.1.4).
func statusAgent(t *testing.T, token, statusBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(statusBody))
	})
	return httptest.NewServer(mux)
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestCreateEmptyNameDerivation(t *testing.T) {
	ctx := context.Background()

	t.Run("hostname from probe", func(t *testing.T) {
		srv := statusAgent(t, "tok", `{"unitActive":"active","clashAlive":true,"version":"0.1.4","hostname":"edge-01","lastHash":"","lastSyncAt":0,"lastResult":"","inbounds":[]}`)
		defer srv.Close()
		svc := NewService(openTestStore(t), nil)
		v, err := svc.Create(ctx, CreateRequest{Address: srv.URL, Token: "tok"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if v.Name != "edge-01" {
			t.Fatalf("Name: got %q, want %q", v.Name, "edge-01")
		}
		if v.Warning != "" {
			t.Fatalf("Warning: got %q, want empty (probe succeeded)", v.Warning)
		}
		if !v.Online {
			t.Fatalf("Online: got false, want true")
		}
	})

	t.Run("old agent without hostname falls back to address host", func(t *testing.T) {
		srv := statusAgent(t, "tok", `{"unitActive":"active","clashAlive":true,"version":"0.1.3","lastHash":"","lastSyncAt":0,"lastResult":"","inbounds":[]}`)
		defer srv.Close()
		svc := NewService(openTestStore(t), nil)
		v, err := svc.Create(ctx, CreateRequest{Address: srv.URL, Token: "tok"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if v.Name != "127.0.0.1" {
			t.Fatalf("Name: got %q, want %q", v.Name, "127.0.0.1")
		}
		if v.Warning != "" {
			t.Fatalf("Warning: got %q, want empty (probe succeeded)", v.Warning)
		}
	})

	t.Run("unreachable agent still creates with IP fallback and warning", func(t *testing.T) {
		svc := NewService(openTestStore(t), nil)
		v, err := svc.Create(ctx, CreateRequest{Address: "127.0.0.1:1", Token: "tok"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if v.Name != "127.0.0.1" {
			t.Fatalf("Name: got %q, want %q", v.Name, "127.0.0.1")
		}
		if v.Warning == "" {
			t.Fatalf("Warning: got empty, want the unreachable warning")
		}
	})

	t.Run("derived name collision is still a conflict", func(t *testing.T) {
		srv := statusAgent(t, "tok", `{"unitActive":"active","clashAlive":true,"version":"0.1.4","hostname":"edge-01","lastHash":"","lastSyncAt":0,"lastResult":"","inbounds":[]}`)
		defer srv.Close()
		svc := NewService(openTestStore(t), nil)
		if _, err := svc.Create(ctx, CreateRequest{Name: "edge-01", Address: srv.URL, Token: "tok"}); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		if _, err := svc.Create(ctx, CreateRequest{Address: srv.URL, Token: "tok"}); err == nil {
			t.Fatalf("second Create: got nil error, want name conflict")
		}
	})

	t.Run("named creation unchanged", func(t *testing.T) {
		srv := statusAgent(t, "tok", `{"unitActive":"active","clashAlive":true,"version":"0.1.4","hostname":"edge-01","lastHash":"","lastSyncAt":0,"lastResult":"","inbounds":[]}`)
		defer srv.Close()
		svc := NewService(openTestStore(t), nil)
		v, err := svc.Create(ctx, CreateRequest{Name: "my-node", Address: srv.URL, Token: "tok"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if v.Name != "my-node" {
			t.Fatalf("Name: got %q, want %q", v.Name, "my-node")
		}
	})
}
