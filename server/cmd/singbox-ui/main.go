// Command singbox-ui is the panel server binary. main is wiring only: it reads
// configuration, constructs the modules (store → auth/core → dash → httpx)
// and connects them through their interfaces. All behaviour lives in the
// internal packages.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"singbox-ui/server/internal/auth"
	"singbox-ui/server/internal/certs"
	"singbox-ui/server/internal/core"
	"singbox-ui/server/internal/coresync"
	"singbox-ui/server/internal/dash"
	"singbox-ui/server/internal/httpx"
	"singbox-ui/server/internal/inbounds"
	"singbox-ui/server/internal/localmachine"
	"singbox-ui/server/internal/logs"
	"singbox-ui/server/internal/nodes"
	"singbox-ui/server/internal/render"
	"singbox-ui/server/internal/settings"
	"singbox-ui/server/internal/store"
	"singbox-ui/server/internal/subs"
	"singbox-ui/server/internal/templates"
	"singbox-ui/server/internal/traffic"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log, cfg); err != nil {
		log.Error("singbox-ui exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, cfg config) error {
	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	// The panel-managed core log file lives here (see coresync); the
	// directory must exist before the first sync restarts the core.
	if err := os.MkdirAll(filepath.Join(cfg.dataDir, "logs"), 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	// --- persistence -------------------------------------------------------
	st, err := store.Open(ctx, filepath.Join(cfg.dataDir, "singbox-ui.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	// --- auth ---------------------------------------------------------------
	authSvc := auth.NewService(st)
	created, username, password, err := authSvc.Bootstrap(ctx)
	if err != nil {
		return fmt.Errorf("auth bootstrap: %w", err)
	}
	if created {
		printFirstRunBanner(username, password)
	}
	go func() { // opportunistic session cleanup
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = authSvc.PruneSessions(ctx)
			}
		}
	}()

	// --- core ---------------------------------------------------------------
	var coreClient core.CoreClient
	switch cfg.coreKind {
	case "fake":
		coreClient = core.NewFakeClient()
		log.Info("core: using fake core (synthetic data)")
	case "clash":
		coreClient = core.NewClashClient(cfg.clashAPI, cfg.clashKey)
		log.Info("core: using sing-box clash api", "base_url", cfg.clashAPI)
	default:
		return fmt.Errorf("unknown SINGBOX_UI_CORE %q (want \"clash\" or \"fake\")", cfg.coreKind)
	}

	// --- dashboard sampler ---------------------------------------------------
	sampler := dash.NewSampler(coreClient, st, log)
	go sampler.Start(ctx)

	// --- certificates ---------------------------------------------------------
	// Guarantee a TLS pair exists as files (generating a self-signed pair on
	// first run), then let the certs module import it as its bootstrap
	// version and serve — and hot-swap — it from its own store.
	certFile, keyFile, err := httpx.EnsurePairFiles(log, httpx.TLSConfig{
		CertFile: cfg.tlsCert,
		KeyFile:  cfg.tlsKey,
		DataDir:  cfg.dataDir,
	})
	if err != nil {
		return err
	}
	certMgr, err := certs.NewManager(cfg.dataDir)
	if err != nil {
		return err
	}
	if err := certMgr.Bootstrap(certFile, keyFile); err != nil {
		return fmt.Errorf("cert bootstrap: %w", err)
	}
	certHandler := certs.NewHandler(certMgr)

	// --- inbounds -------------------------------------------------------------
	// Desired state lives in the store; the service compares it read-only
	// against the core's config file and seeds itself from that file on
	// first run. Nothing here writes to the core.
	inboundSvc := inbounds.NewService(st, cfg.singboxConfig)
	inboundSvc.ImportIfEmpty(ctx, log)
	inboundHandler := inbounds.NewHandler(inboundSvc)

	// --- subscriptions ---------------------------------------------------------
	subsHandler := subs.NewHandler(subs.NewService(st))
	templatesHandler := templates.NewHandler(templates.NewService(st))
	settingsSvc := settings.NewService(st)
	settingsHandler := settings.NewHandler(settingsSvc)
	renderHandler := render.NewHandler(render.NewService(st, settingsSvc))

	// --- sync engine ---------------------------------------------------------
	// The write target defaults to the (read) core config path; the
	// shadow points SINGBOX_UI_SYNC_CONFIG at a sandbox file and leaves
	// SINGBOX_UI_CORE_UNIT empty, so it can never restart the real core.
	syncTarget := cfg.syncConfig
	if syncTarget == "" {
		syncTarget = cfg.singboxConfig
	}
	syncSvc := coresync.NewService(st, settingsSvc, certMgr, coresync.Config{
		Bin:        cfg.singboxBin,
		TargetPath: syncTarget,
		CoreUnit:   cfg.coreUnit,
		ClashAPI:   cfg.clashAPI,
		LogPath:    filepath.Join(cfg.dataDir, "logs", "sing-box.log"),
		Log:        log,
	})
	syncHandler := coresync.NewHandler(syncSvc)

	// --- remote nodes (central push) -------------------------------------
	// The poller keeps agent nodes converged (desired-hash comparison +
	// 30s status probes); manual push-now and clear-block go through the
	// same pusher. No business-handler changes are needed for auto-push:
	// any panel change alters the desired hash, which the poller picks
	// up on its next tick.
	nodesPusher := nodes.NewPusher(st, certMgr, log, filepath.Join(cfg.dataDir, "logs"))
	nodesSvc := nodes.NewService(st, nodesPusher)
	nodesHandler := nodes.NewHandler(nodesSvc)
	go nodesPusher.Start(ctx)

	// Local traffic collector: same Clash+log matching as the
	// node agent, for the local core. Totals persist to a JSON
	// file in the data dir, not a new table.
	localTraffic := traffic.New(cfg.clashAPI, st, filepath.Join(cfg.dataDir, "traffic.json"))
	go localTraffic.Run(ctx)
	trafficSummary := traffic.NewSummaryService(st, nodesSvc, localTraffic)
	trafficHandler := traffic.NewHandler(trafficSummary)
	// Usage sync: every 30s, write the collector's per-subscription
	// totals into the subscriptions table. Without this the
	// subscription page and the client userinfo header read
	// used_up/used_down, which nothing else ever writes, so they
	// stay zero no matter how much traffic flows.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := trafficSummary.SyncUsage(ctx); err != nil {
					slog.Warn("traffic usage sync failed", "error", err)
				}
			}
		}
	}()

	// --- auto-sync: debouncer + drift poll ---------------------------
	// Two pieces, one decision point (the debouncer):
	//   - The debouncer decides WHEN to push: 5s quiet after the
	//     last change, or 30s after the first unpushed change,
	//     whichever comes first. Timer-based, not a 10s tick: a
	//     tick would cut the 5s window into 5-15s.
	//   - The drift poll (5s) discovers FILE-side changes the panel
	//     cannot be told about: it compares Status().DesiredHash
	//     with the last hash it pushed for; a changed hash marks
	//     dirty. Panel-side changes mark dirty directly via the
	//     audit middleware hook, so a panel edit pushes within the
	//     debounce window instead of waiting for the next poll.
	// The run closure checks the enabled switch fresh each run
	// (GetAutoSync; the interval value is IGNORED this round — the
	// debouncer's 5s/30s replace the old interval timing; the
	// settings rows are kept, not deleted or migrated). Disabled
	// clears dirty; the poll re-discovers on re-enable. A busy run
	// (sync already running) keeps dirty and retries soon; an
	// InSync or input-blocked run is a noop that clears dirty.
	// SINGBOX_UI_AUTO_SYNC_MINUTES survives only as the legacy
	// fallback inside settings.GetAutoSync (fresh DBs / the
	// scenario harness).
	debouncer := coresync.NewDebouncer(func() coresync.RunOutcome {
		pctx := context.Background()
		enabled, _, err := settingsSvc.GetAutoSync(pctx, cfg.autoSyncMin)
		if err != nil {
			slog.Warn("auto-sync settings read failed", "error", err)
			return coresync.RunDone // do not spin on a settings error
		}
		if !enabled {
			return coresync.RunDone
		}
		row, ran, err := syncSvc.AutoSync(pctx)
		switch {
		case err != nil:
			slog.Warn("auto-sync failed", "error", err)
			return coresync.RunDone
		case row.Result == "skipped":
			// Busy: another sync holds the lock. Keep dirty,
			// retry soon (do NOT queue).
			return coresync.RunBusy
		case !ran:
			// InSync noop, or input blocked after a rollback:
			// nothing to push.
			return coresync.RunDone
		default:
			slog.Info("auto-sync applied", "result", row.Result, "hash", row.Hash)
			// The debouncer bypasses HTTP, so the audit
			// middleware never sees it — record it explicitly,
			// or the Logs page timeline would have a hole where
			// the core actually changed.
			result, detail := "success", ""
			if row.Result != "applied" && row.Result != "no_change" {
				result = "failed"
				detail = row.Detail
				if detail == "" {
					detail = row.Result
				}
			}
			target := row.Hash
			if len(target) > 8 {
				target = target[:8]
			}
			logs.RecordAudit(context.Background(), st, log, filepath.Join(cfg.dataDir, "logs"), store.AuditRow{
				At:     time.Now().Unix(),
				Action: "自动同步",
				Target: target,
				Result: result,
				Detail: detail,
			})
			return coresync.RunDone
		}
	})
	go debouncer.Start(ctx)
	slog.Info("auto-sync debouncer started", "quiet", "5s", "cap", "30s", "driftPoll", "5s")
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var lastSeen string
		var lastFileHash string
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			pctx := context.Background()
			enabled, _, err := settingsSvc.GetAutoSync(pctx, cfg.autoSyncMin)
			if err != nil {
				slog.Warn("auto-sync drift poll settings read failed", "error", err)
				continue
			}
			if !enabled {
				// Reset: re-enable must re-discover the drift
				// instead of treating it as already seen.
				lastSeen = ""
				lastFileHash = ""
				continue
			}
			// Cheap pre-check: the poll only discovers FILE-side changes
			// (panel changes notify via the audit hook), so if the file
			// bytes are unchanged since the last poll there is no file
			// drift to discover — skip the full Status() (which
			// regenerates + classifies + merges). A failed read is
			// treated as changed (Status handles missing/unreadable).
			if fb, ferr := os.ReadFile(syncTarget); ferr == nil {
				fh := fmt.Sprintf("%x", sha256.Sum256(fb))
				if fh == lastFileHash {
					continue
				}
				lastFileHash = fh
			} else {
				lastFileHash = ""
			}
			status, err := syncSvc.Status(pctx)
			if err != nil {
				slog.Warn("auto-sync drift poll failed", "error", err)
				continue
			}
			if status.InSync {
				lastSeen = status.DesiredHash
				// Converged with a stale input block (operator
				// fixed the file by hand): trigger one run so
				// AutoSync clears the block. The run is a noop
				// otherwise, and the next poll sees an empty
				// block, so this fires at most once per block.
				if baseline, berr := st.GetSyncState(pctx); berr == nil && baseline.BlockedHash != "" {
					debouncer.Notify()
				}
				continue
			}
			if status.DesiredHash != lastSeen {
				lastSeen = status.DesiredHash
				debouncer.Notify()
			}
		}
	}()

	// --- http ----------------------------------------------------------------
	engine := httpx.NewEngine(log)
	// Audit middleware first: it records every mutating /api call
	// (reads excluded) for the Logs page, regardless of route group.
	// Its notify hook marks the debouncer dirty on every successful
	// panel change.
	engine.Use(logs.AuditMiddleware(st, log, filepath.Join(cfg.dataDir, "logs"), debouncer.Notify))
	authHandler := auth.NewHandler(authSvc)
	dashHandler := dash.NewHandler(sampler)
	localHandler := localmachine.NewHandler(cfg.addr)
	logsSvc := logs.NewService(st, filepath.Join(cfg.dataDir, "logs", "sing-box.log"), log)
	// Log following: the panel-managed core log file is ingested into
	// core_logs, so the Logs page serves indexed SELECTs.
	go logsSvc.StartFollow(ctx)
	logsHandler := logs.NewHandler(logsSvc)

	public := engine.Group("/api")
	authHandler.RegisterPublic(public)

	protected := engine.Group("/api")
	protected.Use(httpx.AuthGuard(authSvc, auth.CookieName))
	authHandler.RegisterProtected(protected)
	dashHandler.RegisterProtected(protected)
	localHandler.RegisterProtected(protected)
	certHandler.RegisterProtected(protected)
	inboundHandler.RegisterProtected(protected)
	subsHandler.RegisterProtected(protected)
	templatesHandler.RegisterProtected(protected)
	settingsHandler.RegisterProtected(protected)
	renderHandler.RegisterProtected(protected)
	syncHandler.RegisterProtected(protected)
	nodesHandler.RegisterProtected(protected)
	logsHandler.RegisterProtected(protected)
	trafficHandler.RegisterProtected(protected)

	subPublic := engine.Group("")
	renderHandler.RegisterPublic(subPublic)

	httpx.RegisterStatic(engine)

	return httpx.Serve(ctx, log, cfg.addr, engine, certMgr.GetCertificate, httpx.TLSConfig{
		CertFile: cfg.tlsCert,
		KeyFile:  cfg.tlsKey,
		DataDir:  cfg.dataDir,
	})
}

// printFirstRunBanner prints the generated admin password exactly once, on
// the first start only. The plaintext is never stored or logged again.
func printFirstRunBanner(username, password string) {
	fmt.Println()
	fmt.Println("===============================================================")
	fmt.Println("  singbox-ui first run: initial admin account created")
	fmt.Println("  username: " + username)
	fmt.Println("  password: " + password)
	fmt.Println("  Save this password now — it will NOT be shown again.")
	fmt.Println("===============================================================")
	fmt.Println()
}
