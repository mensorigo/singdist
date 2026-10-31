package coresync

import (
	"log/slog"
	"strings"
	"time"

	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/subs"
)

// Error mirrors the other modules' typed API failure.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }
func errNotFound(what string) *Error {
	return &Error{Status: 404, Code: what + "_not_found", Message: what + " not found"}
}

func NewService(st *store.Store, settingsSvc *settings.Service, certMgr *certs.Manager, cfg Config) *Service {
	clashAddr := strings.TrimPrefix(strings.TrimPrefix(cfg.ClashAPI, "http://"), "https://")
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		st:          st,
		settings:    settingsSvc,
		certs:       certMgr,
		inboundsSvc: inbounds.NewService(st, cfg.TargetPath),
		subsSvc:     subs.NewService(st),
		bin:         cfg.Bin,
		target:      cfg.TargetPath,
		unit:        cfg.CoreUnit,
		clashAddr:   clashAddr,
		clashAPI:    cfg.ClashAPI,
		logPath:     cfg.LogPath,
		logger:      log,
	}
}
func (s *Service) warn(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Warn("coresync: "+msg, args...)
	}
}
func nowUnix() int64 { return time.Now().Unix() }
