package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseAddress(t *testing.T) {
	cases := []struct {
		in         string
		wantScheme string
		wantHost   string
		wantErr    bool
	}{
		{"203.0.113.7:32435", "https", "203.0.113.7:32435", false},
		{"http://127.0.0.1:32435", "http", "127.0.0.1:32435", false},
		{"https://example.com:32435", "https", "example.com:32435", false},
		{"example.com:32435/", "https", "example.com:32435", false},
		{"example.com", "", "", true},
		{"", "", "", true},
		{":32435", "", "", true},
	}
	for _, c := range cases {
		scheme, hostport, err := ParseAddress(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseAddress(%q): expected error, got scheme=%q hostport=%q", c.in, scheme, hostport)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAddress(%q): unexpected error: %v", c.in, err)
			continue
		}
		if scheme != c.wantScheme || hostport != c.wantHost {
			t.Errorf("ParseAddress(%q): got %q %q, want %q %q", c.in, scheme, hostport, c.wantScheme, c.wantHost)
		}
	}
}

func TestPublicHostOf(t *testing.T) {
	cases := []struct {
		publicHost string
		address    string
		want       string
	}{
		{"node.example.com", "203.0.113.7:32435", "node.example.com"},
		{"", "203.0.113.7:32435", "203.0.113.7"},
		{"  ", "http://127.0.0.1:32435", "127.0.0.1"},
		{"", "garbage", "garbage"},
	}
	for _, c := range cases {
		if got := PublicHostOf(c.publicHost, c.address); got != c.want {
			t.Errorf("PublicHostOf(%q, %q): got %q, want %q", c.publicHost, c.address, got, c.want)
		}
	}
}

// fakeAgent is a minimal stand-in for the agent management API.
func fakeAgent(t *testing.T, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"applied","hash":"abc","detail":"ok"}`))
	})
	mux.HandleFunc("/api/agent/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unitActive":"active","clashAlive":true,"version":"0.1.0","lastHash":"abc","lastSyncAt":1000,"lastResult":"applied","inbounds":[{"name":"v1","protocol":"vless-reality","port":443,"source":"push","listening":true}]}`))
	})
	mux.HandleFunc("/api/agent/clear-block", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cleared":true,"previousBlockedHash":""}`))
	})
	return httptest.NewServer(mux)
}

func TestClientPushStatusClearBlock(t *testing.T) {
	srv := fakeAgent(t, "tok")
	defer srv.Close()
	c := NewClient()
	ctx := context.Background()

	res, err := c.Push(ctx, srv.URL, "tok", PushPayload{})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Result != "applied" || res.Hash != "abc" {
		t.Fatalf("Push: got %+v, want applied/abc", res)
	}

	st, err := c.Status(ctx, srv.URL, "tok")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.UnitActive != "active" || !st.ClashAlive || st.Version != "0.1.0" {
		t.Fatalf("Status: got %+v", st)
	}
	if len(st.Inbounds) != 1 || st.Inbounds[0].Name != "v1" ||
		st.Inbounds[0].Listening == nil || !*st.Inbounds[0].Listening {
		t.Fatalf("Status inbounds: got %+v", st.Inbounds)
	}

	cleared, err := c.ClearBlock(ctx, srv.URL, "tok")
	if err != nil {
		t.Fatalf("ClearBlock: %v", err)
	}
	if !cleared {
		t.Fatalf("ClearBlock: got false, want true")
	}
}

func TestClientAuthAndServerErrors(t *testing.T) {
	srv := fakeAgent(t, "tok")
	defer srv.Close()
	c := NewClient()
	ctx := context.Background()

	if _, err := c.Push(ctx, srv.URL, "wrong", PushPayload{}); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("Push with bad token: got err %v, want unauthorized", err)
	}

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer errSrv.Close()
	if _, err := c.Status(ctx, errSrv.URL, "tok"); err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Status on 500: got err %v, want HTTP 500 + boom", err)
	}
}

func TestClientStatusTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(11 * time.Second)
	}))
	defer slow.Close()
	c := NewClient()
	_, err := c.Status(context.Background(), slow.URL, "tok")
	if err == nil {
		t.Fatalf("Status against a 11s-slow server: expected timeout error, got nil")
	}
}
