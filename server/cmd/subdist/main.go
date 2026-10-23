// Command subdist is the panel server binary. main is wiring only: it reads
// configuration, constructs the modules (store → auth/core → dash → httpx)
// and connects them through their interfaces. All behaviour lives in the
// internal packages.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"subdist/server/internal/auth"
	"subdist/server/internal/certs"
	"subdist/server/internal/core"
	"subdist/server/internal/dash"
	"subdist/server/internal/httpx"
	"subdist/server/internal/store"
)

type config struct {
	dataDir  string
	addr     string
	coreKind string
	clashAPI string
	clashKey string
	tlsCert  string
	tlsKey   string
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() config {
	return config{
		dataDir:  envOr("SUBDIST_DATA_DIR", "./data"),
		addr:     envOr("SUBDIST_ADDR", ":8443"),
		coreKind: envOr("SUBDIST_CORE", "clash"),
		clashAPI: envOr("SUBDIST_CLASH_API", "http://127.0.0.1:9090"),
		clashKey: os.Getenv("SUBDIST_CLASH_SECRET"),
		tlsCert:  os.Getenv("SUBDIST_TLS_CERT"),
		tlsKey:   os.Getenv("SUBDIST_TLS_KEY"),
	}
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log, cfg); err != nil {
		log.Error("subdist exited with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, cfg config) error {
	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// --- persistence -------------------------------------------------------
	st, err := store.Open(ctx, filepath.Join(cfg.dataDir, "subdist.db"))
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
		return fmt.Errorf("unknown SUBDIST_CORE %q (want \"clash\" or \"fake\")", cfg.coreKind)
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

	// --- http ----------------------------------------------------------------
	engine := httpx.NewEngine(log)
	authHandler := auth.NewHandler(authSvc)
	dashHandler := dash.NewHandler(sampler)

	public := engine.Group("/api")
	authHandler.RegisterPublic(public)

	protected := engine.Group("/api")
	protected.Use(httpx.AuthGuard(authSvc, auth.CookieName))
	authHandler.RegisterProtected(protected)
	dashHandler.RegisterProtected(protected)
	certHandler.RegisterProtected(protected)

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
	fmt.Println("  SubDist first run: initial admin account created")
	fmt.Println("  username: " + username)
	fmt.Println("  password: " + password)
	fmt.Println("  Save this password now — it will NOT be shown again.")
	fmt.Println("===============================================================")
	fmt.Println()
}
