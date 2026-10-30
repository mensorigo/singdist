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
//	SINGBOX_AGENT_ENV_FILE        env file whose SINGBOX_AGENT_TOKEN= line a
//	                              successful pairing keeps in sync (optional)
//
// Token resolution at startup: <dataDir>/agent-token wins over
// SINGBOX_AGENT_TOKEN. When neither exists the agent starts unpaired
// and prints one pairing code to the log (valid 5 minutes,
// one-time); a token-holding agent never auto-generates a code on
// restart. Run `singbox-agent pair-code` (no service start) to
// generate a fresh code on demand from the node terminal.
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
	// pair-code is an operator command, not a service start: it
	// generates a fresh pairing code, prints it to stdout and exits.
	// It needs no token and bypasses the HTTP 60-second interval
	// (the operator is on the node itself), but it still invalidates
	// any pending code. No-arg startup below is unchanged.
	if len(os.Args) > 1 && os.Args[1] == "pair-code" {
		dataDir := envOr("SINGBOX_AGENT_DATA_DIR", "./agent-data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			logger.Fatalf("create data dir: %v", err)
		}
		code, err := agent.NewPairingManager(dataDir).Generate()
		if err != nil {
			logger.Fatalf("generate pairing code: %v", err)
		}
		fmt.Println(code)
		return
	}
	dataDir := envOr("SINGBOX_AGENT_DATA_DIR", "./agent-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		logger.Fatalf("create data dir: %v", err)
	}
	// Token file wins over the environment token; neither means the
	// agent starts unpaired (no fatal: pairing is how the first
	// token is established).
	token, err := agent.LoadToken(dataDir, os.Getenv("SINGBOX_AGENT_TOKEN"))
	if err != nil {
		logger.Fatalf("load token: %v", err)
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
	srv.SetEnvFile(os.Getenv("SINGBOX_AGENT_ENV_FILE"))
	if token == "" {
		// Unpaired startup: generate one pairing code and print it
		// to the log (journal under systemd) for the operator to
		// enter in the panel. A token-holding agent never reaches
		// this branch, so restarts never auto-generate a code.
		code, err := srv.Pairing().Generate()
		if err != nil {
			logger.Fatalf("generate pairing code: %v", err)
		}
		logger.Printf("no node token: pairing code (valid 5 minutes, one-time): %s", code)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ag.StartTraffic(ctx)
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
