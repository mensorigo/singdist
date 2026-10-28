package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// freePort binds an ephemeral port, closes it, and returns the
// number. A race (someone else binding it in between) is negligible
// for these tests, and the managed/reserved assertions below do not
// depend on the port staying free.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func checkPortAgent(t *testing.T, target string, mgmtPort int) *Agent {
	t.Helper()
	a, err := New(Config{DataDir: t.TempDir(), Target: target, MgmtPort: mgmtPort})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCheckPortListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	a := checkPortAgent(t, filepath.Join(t.TempDir(), "no-such-config.json"), 0)

	if got := a.CheckPort(port, transportTCP, "tcp"); !got.Occupied || got.Kind != "listening" {
		t.Errorf("TCP listener: expected occupied/listening, got %+v", got)
	}
	if got := a.CheckPort(port, transportUDP, "udp"); got.Occupied {
		t.Errorf("TCP listener must not block UDP: %+v", got)
	}
	ln.Close()
	if got := a.CheckPort(port, transportTCP, "tcp"); got.Occupied {
		t.Errorf("closed listener: expected free, got %+v", got)
	}
}

func TestCheckPortManaged(t *testing.T) {
	pVless := freePort(t)
	pSS := freePort(t)
	pUnknown := freePort(t)
	doc := fmt.Sprintf(`{"inbounds":[
		{"type":"vless","tag":"m1","listen_port":%d},
		{"type":"shadowsocks","tag":"ss1","listen_port":%d},
		{"type":"socks","tag":"sx","listen_port":%d}
	]}`, pVless, pSS, pUnknown)
	target := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(target, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	a := checkPortAgent(t, target, 0)

	if got := a.CheckPort(pVless, transportTCP, "tcp"); !got.Occupied || got.Kind != "managed" || got.OccupiedBy != "m1" {
		t.Errorf("vless TCP: expected occupied/managed by m1, got %+v", got)
	}
	if got := a.CheckPort(pVless, transportUDP, "udp"); got.Occupied {
		t.Errorf("vless must not block UDP: %+v", got)
	}
	if got := a.CheckPort(pSS, transportTCP, "tcp"); !got.Occupied {
		t.Errorf("ss TCP: expected occupied, got %+v", got)
	}
	if got := a.CheckPort(pSS, transportUDP, "udp"); !got.Occupied {
		t.Errorf("ss UDP: expected occupied, got %+v", got)
	}
	if got := a.CheckPort(pUnknown, transportTCP, "tcp"); !got.Occupied {
		t.Errorf("unknown type: expected conservative occupied, got %+v", got)
	}
}

func TestCheckPortReserved(t *testing.T) {
	mgmt := freePort(t)
	a := checkPortAgent(t, filepath.Join(t.TempDir(), "no-such-config.json"), mgmt)
	if got := a.CheckPort(mgmt, transportUDP, "udp"); !got.Occupied {
		t.Errorf("reserved mgmt port must block any transport: %+v", got)
	}
	if got := a.CheckPort(2826, transportTCP, "tcp"); !got.Occupied || got.Kind != "reserved" || !strings.Contains(got.Reason, "SSH") {
		t.Errorf("2826: expected reserved SSH, got %+v", got)
	}
}

func TestHandleCheckPort(t *testing.T) {
	a := checkPortAgent(t, filepath.Join(t.TempDir(), "no-such-config.json"), 0)
	s := NewServer(a, "tok", ":0", "", "", t.TempDir(), true, nil)
	h := s.auth(s.handleCheckPort)

	call := func(method, url, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}

	free := freePort(t)
	rec := call("GET", fmt.Sprintf("/api/agent/check-port?port=%d&transport=tcp", free), "Bearer tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var chk PortCheck
	if err := json.Unmarshal(rec.Body.Bytes(), &chk); err != nil {
		t.Fatal(err)
	}
	if chk.Port != free || chk.Transport != "tcp" || chk.Occupied {
		t.Errorf("unexpected check result: %+v", chk)
	}

	if rec := call("GET", "/api/agent/check-port?port=1", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no auth: expected 401, got %d", rec.Code)
	}
	if rec := call("GET", "/api/agent/check-port?port=0", "Bearer tok"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad port: expected 400, got %d", rec.Code)
	}
	if rec := call("GET", "/api/agent/check-port?port=80&transport=sctp", "Bearer tok"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad transport: expected 400, got %d", rec.Code)
	}
	if rec := call("POST", "/api/agent/check-port?port=80", "Bearer tok"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: expected 405, got %d", rec.Code)
	}
}
