// Command singbox-agent is the standalone node agent: it receives
// inbound definitions pushed from the central panel, turns them
// into a sing-box core config with its own inbound logic, and
// applies that config to the local core.
//
// Configuration (environment, SINGBOX_AGENT_ prefix):
//
//	SINGBOX_AGENT_ADDR            management API listen address (default :32435)
//	SINGBOX_AGENT_TOKEN           node token (REQUIRED; the only credential)
//	SINGBOX_AGENT_DATA_DIR        agent state dir (default ./agent-data)
//	SINGBOX_AGENT_CORE_CONFIG     core config path (default /etc/sing-box/config.json)
//	SINGBOX_AGENT_BIN             sing-box binary for `check` (default /usr/bin/sing-box)
//	SINGBOX_AGENT_CORE_UNIT       systemd unit (default sing-box)
//	SINGBOX_AGENT_NO_RESTART      "1" disables core restarts (write + validate only)
//	SINGBOX_AGENT_CLASH_API       Clash API base URL (default http://127.0.0.1:9090)
//	SINGBOX_AGENT_LOG_PATH        core log.output path (default <dataDir>/logs/sing-box.log)
//	SINGBOX_AGENT_LOCAL_INBOUNDS  optional local inbounds file path
//	SINGBOX_AGENT_TLS_CERT/KEY    management TLS cert/key (default: auto self-signed)
//	SINGBOX_AGENT_TLS_OFF         "1" serves plain HTTP (tests/dev only)
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"singbox-agent/internal/agent"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func addrPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

func main() {
	logger := log.New(os.Stderr, "agent: ", log.LstdFlags)
	token := os.Getenv("SINGBOX_AGENT_TOKEN")
	if token == "" {
		logger.Fatal("SINGBOX_AGENT_TOKEN is required")
	}
	dataDir := envOr("SINGBOX_AGENT_DATA_DIR", "./agent-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		logger.Fatalf("create data dir: %v", err)
	}
	addr := envOr("SINGBOX_AGENT_ADDR", ":32435")
	unit := envOr("SINGBOX_AGENT_CORE_UNIT", "sing-box")
	if os.Getenv("SINGBOX_AGENT_NO_RESTART") == "1" {
		unit = ""
	}
	logPath := os.Getenv("SINGBOX_AGENT_LOG_PATH")
	if logPath == "" {
		logPath = filepath.Join(dataDir, "logs", "sing-box.log")
	}
	cfg := agent.Config{
		DataDir:       dataDir,
		Target:        envOr("SINGBOX_AGENT_CORE_CONFIG", "/etc/sing-box/config.json"),
		Bin:           envOr("SINGBOX_AGENT_BIN", "/usr/bin/sing-box"),
		Unit:          unit,
		ClashAddr:     envOr("SINGBOX_AGENT_CLASH_API", "http://127.0.0.1:9090"),
		LogPath:       logPath,
		MgmtPort:      addrPort(addr),
		LocalInbounds: os.Getenv("SINGBOX_AGENT_LOCAL_INBOUNDS"),
	}
	ag, err := agent.New(cfg)
	if err != nil {
		logger.Fatalf("init agent: %v", err)
	}
	srv := agent.NewServer(ag, token, addr,
		os.Getenv("SINGBOX_AGENT_TLS_CERT"), os.Getenv("SINGBOX_AGENT_TLS_KEY"),
		dataDir, os.Getenv("SINGBOX_AGENT_TLS_OFF") == "1", logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	unitDesc := unit
	if unitDesc == "" {
		unitDesc = "(no restart)"
	}
	fmt.Fprintf(os.Stderr, "agent: listening %s target=%s unit=%s data=%s clash=%s\n",
		addr, cfg.Target, unitDesc, dataDir, cfg.ClashAddr)
	if err := srv.Run(ctx); err != nil {
		logger.Fatalf("serve: %v", err)
	}
}
