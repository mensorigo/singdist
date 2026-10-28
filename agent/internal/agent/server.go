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
	"time"
)

// Server is the agent's management API. Every endpoint requires
// the node token (Bearer); the token is the only credential —
// there are no human accounts, sessions or passwords here.
type Server struct {
	agent   *Agent
	token   string
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
	return &Server{agent: agent, token: token, addr: addr, tlsCert: tlsCert, tlsKey: tlsKey, tlsOff: tlsOff, dataDir: dataDir, log: logger}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		want := "Bearer " + s.token
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
