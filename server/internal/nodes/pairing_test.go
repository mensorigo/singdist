package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonDecode decodes a request JSON body, ignoring errors (a bad
// body simply yields the zero request, which the fake agent rejects).
func jsonDecode(r *http.Request, v any) {
	_ = json.NewDecoder(r.Body).Decode(v)
}

func TestClientPairing(t *testing.T) {
	t.Run("request ok and retry", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/agent/request-pairing-code", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":false,"retry_after":30}`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		c := NewClient()
		ok, retry, err := c.RequestPairingCode(context.Background(), srv.URL)
		if err != nil {
			t.Fatalf("RequestPairingCode: %v", err)
		}
		if ok || retry != 30 {
			t.Fatalf("RequestPairingCode: got ok=%v retry=%d, want false/30", ok, retry)
		}
	})
	t.Run("pair success and generic failure", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/agent/pair", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Code string `json:"code"`
			}
			jsonDecode(r, &req)
			w.Header().Set("Content-Type", "application/json")
			if req.Code == "ABCDEFGH" {
				_, _ = w.Write([]byte(`{"token":"fresh-token"}`))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"配对码无效或已过期"}`))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		c := NewClient()
		tok, err := c.Pair(context.Background(), srv.URL, "ABCDEFGH")
		if err != nil || tok != "fresh-token" {
			t.Fatalf("Pair success: got token %q err %v", tok, err)
		}
		if _, err := c.Pair(context.Background(), srv.URL, "ZZZZZZZZ"); !errors.Is(err, ErrPairingInvalid) {
			t.Fatalf("Pair failure: got err %v, want ErrPairingInvalid", err)
		}
	})
	t.Run("old agent 404", func(t *testing.T) {
		srv := httptest.NewServer(http.NewServeMux())
		defer srv.Close()
		c := NewClient()
		if _, _, err := c.RequestPairingCode(context.Background(), srv.URL); !errors.Is(err, ErrPairingUnsupported) {
			t.Fatalf("RequestPairingCode 404: got err %v, want ErrPairingUnsupported", err)
		}
		if _, err := c.Pair(context.Background(), srv.URL, "ABCDEFGH"); !errors.Is(err, ErrPairingUnsupported) {
			t.Fatalf("Pair 404: got err %v, want ErrPairingUnsupported", err)
		}
	})
}

func TestServiceCreateWithPairing(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/pair", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code string `json:"code"`
		}
		jsonDecode(r, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Code == "ABCDEFGH" {
			_, _ = w.Write([]byte(`{"token":"fresh-token"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"配对码无效或已过期"}`))
	})
	mux.HandleFunc("/api/agent/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unitActive":"active","clashAlive":true,"version":"0.2.0","hostname":"pair-01","lastHash":"","lastSyncAt":0,"lastResult":"","inbounds":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("pairing code creates node with the fresh token", func(t *testing.T) {
		st := openTestStore(t)
		svc := NewService(st, nil)
		v, err := svc.Create(ctx, CreateRequest{Name: "pair-node", Address: srv.URL, PairingCode: "ABCDEFGH"})
		if err != nil {
			t.Fatalf("Create with pairing code: %v", err)
		}
		if v.Warning != "" {
			t.Fatalf("Warning: got %q, want empty (pairing proved reachability)", v.Warning)
		}
		row, err := st.GetNode(ctx, v.ID)
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if row.Token != "fresh-token" {
			t.Fatalf("stored token: got %q, want the paired token", row.Token)
		}
	})
	t.Run("invalid code is rejected", func(t *testing.T) {
		svc := NewService(openTestStore(t), nil)
		if _, err := svc.Create(ctx, CreateRequest{Name: "bad-node", Address: srv.URL, PairingCode: "ZZZZZZZZ"}); err == nil || !strings.Contains(err.Error(), "配对码无效或已过期") {
			t.Fatalf("Create with bad code: got err %v, want the generic pairing message", err)
		}
	})
	t.Run("neither code nor token is still rejected", func(t *testing.T) {
		svc := NewService(openTestStore(t), nil)
		if _, err := svc.Create(ctx, CreateRequest{Name: "no-cred", Address: srv.URL}); err == nil {
			t.Fatalf("Create without code or token: got nil error, want a rejection")
		}
	})
}

func TestServiceUpdateWithPairing(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/pair", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code string `json:"code"`
		}
		jsonDecode(r, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Code == "ABCDEFGH" {
			_, _ = w.Write([]byte(`{"token":"rotated-token"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"配对码无效或已过期"}`))
	})
	mux.HandleFunc("/api/agent/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unitActive":"active","clashAlive":true,"version":"0.2.0","lastHash":"","lastSyncAt":0,"lastResult":"","inbounds":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	st := openTestStore(t)
	svc := NewService(st, nil)
	// The status handler accepts any token here, so manual creation
	// succeeds; the pairing step below is what rotates the token.
	v, err := svc.Create(ctx, CreateRequest{Name: "re-pair", Address: srv.URL, Token: "manual-token"})
	if err != nil {
		t.Fatalf("manual Create: %v", err)
	}
	code := "ABCDEFGH"
	if _, err := svc.Update(ctx, v.ID, UpdateRequest{PairingCode: &code}); err != nil {
		t.Fatalf("Update with pairing code: %v", err)
	}
	row, err := st.GetNode(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if row.Token != "rotated-token" {
		t.Fatalf("stored token after re-pair: got %q, want the rotated token", row.Token)
	}
}
