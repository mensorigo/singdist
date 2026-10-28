// Package nodes is the central side of multi-node push: it stores remote
// agent nodes, builds push payloads from the panel's desired state, pushes
// them to agents, polls agent status, and renders multi-node
// subscriptions. It never imports the agent module (a separate module):
// the agent API shapes below are local mirrors of that contract.
package nodes

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// --- agent API mirrors (the contract; field names must match the agent) ---

// PushUser is one agent inbound user. vless/tuic carry Name+UUID;
// password protocols carry Name only; shadowsocks ignores users.
type PushUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

// PushInbound is one inbound definition pushed to an agent.
type PushInbound struct {
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Port     int             `json:"port"`
	Enabled  bool            `json:"enabled"`
	Params   json.RawMessage `json:"params"`
	Users    []PushUser      `json:"users,omitempty"`
}

// PushCert is the PEM certificate embedded for TLS inbounds.
type PushCert struct {
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

// PushPayload is the POST /api/agent/sync body.
type PushPayload struct {
	Inbounds   []PushInbound `json:"inbounds"`
	PublicHost string        `json:"publicHost,omitempty"`
	Cert       *PushCert     `json:"cert,omitempty"`
}

// PushResult is the agent's sync response.
type PushResult struct {
	Result string   `json:"result"`
	Hash   string   `json:"hash"`
	Detail string   `json:"detail"`
	Notes  []string `json:"notes,omitempty"`
}

// StatusInbound is one inbound in the agent's status report.
type StatusInbound struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port"`
	Source    string `json:"source"`
	Listening *bool  `json:"listening,omitempty"`
}

// AgentStatus is the GET /api/agent/status response.
type AgentStatus struct {
	UnitActive  string          `json:"unitActive"`
	ClashAlive  bool            `json:"clashAlive"`
	Version     string          `json:"version,omitempty"`
	LastHash    string          `json:"lastHash"`
	LastSyncAt  int64           `json:"lastSyncAt"`
	LastResult  string          `json:"lastResult"`
	BlockedHash string          `json:"blockedHash,omitempty"`
	Inbounds    []StatusInbound `json:"inbounds"`
}

// --- address parsing --------------------------------------------------------

// ParseAddress splits a node address into scheme and host:port. A bare
// host:port means https (the agent serves TLS by default); an explicit
// http:// prefix selects plaintext (tests and dev agents with TLS off).
func ParseAddress(address string) (scheme, hostport string, err error) {
	scheme = "https"
	rest := strings.TrimSpace(address)
	if strings.HasPrefix(rest, "http://") {
		scheme = "http"
		rest = strings.TrimPrefix(rest, "http://")
	} else if strings.HasPrefix(rest, "https://") {
		rest = strings.TrimPrefix(rest, "https://")
	}
	rest = strings.TrimSuffix(rest, "/")
	host, port, err := net.SplitHostPort(rest)
	if err != nil {
		return "", "", fmt.Errorf("invalid address %q: %w", address, err)
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("invalid address %q: host and port required", address)
	}
	return scheme, rest, nil
}

// PublicHostOf returns the subscription-facing host for a node: the
// configured public host, or the address host when none is set.
func PublicHostOf(publicHost, address string) string {
	if strings.TrimSpace(publicHost) != "" {
		return strings.TrimSpace(publicHost)
	}
	_, hostport, err := ParseAddress(address)
	if err != nil {
		return address
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}

// --- HTTP client ------------------------------------------------------------

const (
	pushTimeout      = 120 * time.Second
	statusTimeout    = 10 * time.Second
	clearTimeout     = 10 * time.Second
	checkPortTimeout = 10 * time.Second
	maxResponseBytes = 4 << 20
)

// ErrCheckPortUnsupported means the agent answered 404 for the
// check-port endpoint: it predates that endpoint and must be
// upgraded before per-machine port checks can run against it.
var ErrCheckPortUnsupported = errors.New("agent does not support check-port (upgrade the agent)")

// Client talks to one agent's management API. v1 always skips TLS
// verification (the token is the auth; TLS only encrypts). The tlsMode
// parameter is accepted for the future pin/mtls modes and rejected
// unless it is skip-verify.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // v1: token is the auth
		},
	}}
}

func (c *Client) baseURL(address string) (string, error) {
	scheme, hostport, err := ParseAddress(address)
	if err != nil {
		return "", err
	}
	return scheme + "://" + hostport, nil
}

func (c *Client) do(ctx context.Context, method, url, token string, body []byte) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, data, nil
}

func errForStatus(status int, body []byte) error {
	if status == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: bad node token")
	}
	return fmt.Errorf("agent returned HTTP %d: %s", status, truncate(string(body), 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Push sends a payload to the agent and returns its result.
func (c *Client) Push(ctx context.Context, address, token string, payload PushPayload) (*PushResult, error) {
	base, err := c.baseURL(address)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	status, data, err := c.do(ctx, http.MethodPost, base+"/api/agent/sync", token, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errForStatus(status, data)
	}
	var res PushResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("bad agent push response: %w", err)
	}
	return &res, nil
}

// Status fetches the agent's live status.
func (c *Client) Status(ctx context.Context, address, token string) (*AgentStatus, error) {
	base, err := c.baseURL(address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	status, data, err := c.do(ctx, http.MethodGet, base+"/api/agent/status", token, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errForStatus(status, data)
	}
	var st AgentStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("bad agent status response: %w", err)
	}
	return &st, nil
}

// AgentPortCheck mirrors the agent's check-port answer. Kind is ""
// when free, else reserved | managed | listening; OccupiedBy is the
// managed inbound's tag when Kind is managed.
type AgentPortCheck struct {
	Port       int    `json:"port"`
	Transport  string `json:"transport"`
	Occupied   bool   `json:"occupied"`
	Reason     string `json:"reason"`
	Kind       string `json:"kind"`
	OccupiedBy string `json:"occupiedBy"`
}

// CheckPort asks the agent whether a port is free on its machine for
// a transport (tcp | udp | both). A 404 maps to
// ErrCheckPortUnsupported (the agent predates the endpoint).
func (c *Client) CheckPort(ctx context.Context, address, token string, port int, transport string) (*AgentPortCheck, error) {
	base, err := c.baseURL(address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, checkPortTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/agent/check-port?port=%d&transport=%s", base, port, transport)
	status, data, err := c.do(ctx, http.MethodGet, url, token, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrCheckPortUnsupported
	}
	if status != http.StatusOK {
		return nil, errForStatus(status, data)
	}
	var chk AgentPortCheck
	if err := json.Unmarshal(data, &chk); err != nil {
		return nil, fmt.Errorf("bad agent check-port response: %w", err)
	}
	return &chk, nil
}

// ClearBlock asks the agent to clear its blocked input hash.
func (c *Client) ClearBlock(ctx context.Context, address, token string) (bool, error) {
	base, err := c.baseURL(address)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, clearTimeout)
	defer cancel()
	status, data, err := c.do(ctx, http.MethodPost, base+"/api/agent/clear-block", token, nil)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, errForStatus(status, data)
	}
	var res struct {
		Cleared bool `json:"cleared"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return false, fmt.Errorf("bad agent clear-block response: %w", err)
	}
	return res.Cleared, nil
}
