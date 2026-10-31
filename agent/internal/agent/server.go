package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Server is the agent's management API. Every endpoint requires
// the node token (Bearer) except the two pairing endpoints, which
// must stay open precisely because the panel may have lost its
// token; the token is the only credential — there are no human
// accounts, sessions or passwords here.
//
// The token is mutable: a successful pairing rotates it (token
// file, env file and memory together). An empty token means the
// agent is unpaired: every authenticated endpoint rejects, and only
// pairing can establish a token.
type Server struct {
	agent   *Agent
	token   string
	tokenMu sync.RWMutex
	pairing *PairingManager
	envFile string
	addr    string
	tlsCert string
	tlsKey  string
	tlsOff  bool
	dataDir string
	log     *log.Logger
}

// NewServer builds the management API server.
func NewServer(agent *Agent, token, addr, tlsCert, tlsKey, dataDir string, tlsOff bool, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(os.Stderr, "agent: ", log.LstdFlags)
	}
	return &Server{agent: agent, token: token, pairing: NewPairingManager(dataDir), addr: addr, tlsCert: tlsCert, tlsKey: tlsKey, tlsOff: tlsOff, dataDir: dataDir, log: logger}
}

// Pairing exposes the server's pairing manager (used at startup to
// generate the first code when no token exists yet).
func (s *Server) Pairing() *PairingManager { return s.pairing }

// SetEnvFile records the env file whose SINGBOX_AGENT_TOKEN= line a
// successful pairing keeps in sync with the token file. Empty
// means "no env file to sync".
func (s *Server) SetEnvFile(path string) { s.envFile = path }

func (s *Server) tokenValue() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.token
}

func (s *Server) setToken(token string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	s.token = token
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.tokenValue()
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + token
		if len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleSync receives a push. The apply runs on a background
// context (a client disconnect must not abort an apply
// mid-restart); the pipeline itself is bounded by its 90s
// backstop, and the result is also visible via /status.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var push Push
	if err := json.Unmarshal(body, &push); err != nil {
		http.Error(w, "invalid push JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	res := s.agent.ApplyPush(context.Background(), push)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.agent.Status(r.Context()))
}

func (s *Server) handleCheckPort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}
	tr, label, ok := ParseCheckTransport(r.URL.Query().Get("transport"))
	if !ok {
		http.Error(w, "invalid transport (tcp|udp|both)", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.agent.CheckPort(port, tr, label))
}

func (s *Server) handleCoreInbounds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	inbounds, err := s.agent.CoreInbounds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if inbounds == nil {
		inbounds = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"inbounds": inbounds})
}

// handleRequestPairingCode is the panel's "发送请求" button: it asks
// the agent to generate a fresh pairing code. It needs no token
// (the panel may have lost its token — that is the whole point),
// and it NEVER returns the code: the code is printed to the agent
// log (journal under systemd) for the operator to read on the node
// and enter in the panel. Within 60 seconds of the last generation
// it refuses and reports retry_after, without regenerating or
// invalidating the pending code.
func (s *Server) handleRequestPairingCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code, retryAfter, err := s.pairing.Request()
	if err != nil {
		http.Error(w, "pairing request failed", http.StatusInternalServerError)
		return
	}
	if code == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "retry_after": retryAfter})
		return
	}
	// The ONLY place a pairing code is printed: the agent log, which
	// the operator reads on the node itself. The long-term token is
	// never printed anywhere.
	s.log.Printf("pairing code (valid 5 minutes, one-time): %s", code)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "retry_after": 0})
}

// handlePair verifies a pairing code and, only on success, rotates
// the long-term token: the new token is written to the token file
// (and the env file, when configured) before it is adopted in
// memory, so a crash mid-rotation restarts into the new token, not
// a half-rotated state. The old token stays usable until that
// point; a failed or expired code leaves it untouched. Failures
// always return the same generic message, never the reason.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "配对码无效或已过期"})
		return
	}
	if !s.pairing.Verify(req.Code) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "配对码无效或已过期"})
		return
	}
	oldToken := s.tokenValue()
	newToken, err := GenerateLongToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "配对失败，请重试"})
		return
	}
	if err := WriteTokenFile(s.dataDir, newToken); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "配对失败，请重试"})
		return
	}
	if s.envFile != "" {
		if err := SyncEnvFile(s.envFile, newToken); err != nil {
			// Roll the token file back so memory, file and env
			// stay on the old token instead of half-rotating.
			if oldToken == "" {
				RemoveTokenFile(s.dataDir)
			} else {
				_ = WriteTokenFile(s.dataDir, oldToken)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "配对失败，请重试"})
			return
		}
	}
	s.setToken(newToken)
	writeJSON(w, http.StatusOK, map[string]any{"token": newToken})
}

func (s *Server) handleClearBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	prev := s.agent.ClearBlock()
	writeJSON(w, http.StatusOK, map[string]any{"cleared": prev != "", "previousBlockedHash": prev})
}

// Run serves the management API until ctx is cancelled. TLS is on
// by default: explicit cert/key files win, otherwise a self-signed
// certificate is generated into the data dir on first start.
// tlsOff is for tests and local development only.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/sync", s.auth(s.handleSync))
	mux.HandleFunc("/api/agent/status", s.auth(s.handleStatus))
	mux.HandleFunc("/api/agent/clear-block", s.auth(s.handleClearBlock))
	mux.HandleFunc("/api/agent/check-port", s.auth(s.handleCheckPort))
	mux.HandleFunc("/api/agent/inbounds", s.auth(s.handleCoreInbounds))
	mux.HandleFunc("/api/agent/request-pairing-code", s.handleRequestPairingCode)
	mux.HandleFunc("/api/agent/pair", s.handlePair)
	srv := &http.Server{
		Addr:        s.addr,
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	if s.tlsOff {
		s.log.Printf("WARNING: management API TLS is OFF (tests/dev only)")
		return srv.ListenAndServe()
	}
	certFile, keyFile := s.tlsCert, s.tlsKey
	if certFile == "" || keyFile == "" {
		certFile = filepath.Join(s.dataDir, "agent-mgmt-cert.pem")
		keyFile = filepath.Join(s.dataDir, "agent-mgmt-key.pem")
		if err := ensureSelfSigned(certFile, keyFile); err != nil {
			return fmt.Errorf("agent: management TLS cert: %w", err)
		}
	}
	return srv.ListenAndServeTLS(certFile, keyFile)
}
