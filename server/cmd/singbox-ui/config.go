package main

import (
	"fmt"
	"os"

)

type config struct {
	dataDir       string
	addr          string
	coreKind      string
	clashAPI      string
	clashKey      string
	tlsCert       string
	tlsKey        string
	singboxConfig string
	singboxBin    string
	syncConfig    string
	coreUnit      string
	autoSyncMin   int
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 {
			return n
		}
	}
	return fallback
}

func loadConfig() config {
	return config{
		dataDir:       envOr("SINGBOX_UI_DATA_DIR", "./data"),
		addr:          envOr("SINGBOX_UI_ADDR", ":8443"),
		coreKind:      envOr("SINGBOX_UI_CORE", "clash"),
		clashAPI:      envOr("SINGBOX_UI_CLASH_API", "http://127.0.0.1:9090"),
		clashKey:      os.Getenv("SINGBOX_UI_CLASH_SECRET"),
		tlsCert:       os.Getenv("SINGBOX_UI_TLS_CERT"),
		tlsKey:        os.Getenv("SINGBOX_UI_TLS_KEY"),
		singboxConfig: envOr("SINGBOX_UI_SINGBOX_CONFIG", "/etc/sing-box/config.json"),
		singboxBin:    envOr("SINGBOX_UI_SINGBOX_BIN", "/usr/bin/sing-box"),
		syncConfig:    os.Getenv("SINGBOX_UI_SYNC_CONFIG"),
		coreUnit:      os.Getenv("SINGBOX_UI_CORE_UNIT"),
		autoSyncMin:   envInt("SINGBOX_UI_AUTO_SYNC_MINUTES", 0),
	}
}
