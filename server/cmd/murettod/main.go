// Command murettod is the Muretto sync API server. Configuration comes from
// environment variables; see server/README.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/tolgayayci/muretto/server/internal/api"
	"github.com/tolgayayci/muretto/server/internal/config"
	"github.com/tolgayayci/muretto/server/internal/events"
	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/store/memstore"
	"github.com/tolgayayci/muretto/server/internal/store/pgstore"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("murettod exiting", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := events.NewHub()
	var st store.Store
	publishLocal := false
	if cfg.DatabaseURL == "" {
		log.Warn("MURETTO_DATABASE_URL is not set: using the IN-MEMORY store. All data is lost on restart. Do not use this in production.")
		st = memstore.New()
		publishLocal = true
	} else {
		if err := checkListenURL(cfg.DatabaseListenURL); err != nil {
			return err
		}
		pg, applied, err := pgstore.Open(ctx, cfg.DatabaseURL, pgstore.Options{MaxConns: cfg.DBMaxConns})
		if err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		log.Info("postgres ready", "migrations_applied", applied)
		go pg.Listen(ctx, cfg.DatabaseListenURL, hub.Publish, log)
		st = pg
	}
	defer st.Close()

	if cfg.DevLogin {
		log.Warn("MURETTO_DEV_LOGIN=1: DEV-ONLY login bypass is ON. Anyone who can reach " + cfg.Addr + " can sign in as any user. Never enable this in production.")
	}
	if cfg.SupabaseURL == "" {
		log.Warn("MURETTO_SUPABASE_URL is not set: dashboard sign-in with Supabase is disabled; only CLI tokens authenticate (dev login)")
	} else {
		log.Info("supabase auth", "issuer", cfg.SupabaseURL+"/auth/v1", "legacy_hs256", cfg.SupabaseJWTSecret != "")
	}
	for _, k := range []string{"MURETTO_GITHUB_CLIENT_ID", "MURETTO_GITHUB_CLIENT_SECRET"} {
		if os.Getenv(k) != "" {
			log.Warn(k + " is set but no longer used: dashboard sign-in is Supabase Auth (MURETTO_SUPABASE_URL)")
		}
	}

	srv, err := api.New(api.Options{Config: cfg, Store: st, Hub: hub, PublishLocal: publishLocal, Logger: log})
	if err != nil {
		return err
	}
	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second, // SSE streams clear their own deadline
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("murettod listening", "addr", cfg.Addr, "version", version, "public_url", cfg.PublicURL, "dashboard_url", cfg.DashboardURL)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	return hs.Shutdown(sctx)
}

// checkListenURL refuses a transaction-mode pooler for LISTEN.
func checkListenURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("MURETTO_DATABASE_LISTEN_URL is not a valid URL")
	}
	if p, _ := strconv.Atoi(u.Port()); pgstore.IsTransactionPooler(uint16(p)) {
		return errors.New("LISTEN/NOTIFY does not work through the transaction-mode pooler (port 6543): set MURETTO_DATABASE_LISTEN_URL to the session-mode pooler (port 5432) or a direct connection")
	}
	return nil
}
