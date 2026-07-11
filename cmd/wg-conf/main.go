package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/user/wg-conf/internal/devsetup"
	"github.com/user/wg-conf/internal/api"
	"github.com/user/wg-conf/internal/config"
	"github.com/user/wg-conf/internal/monitor"
	"github.com/user/wg-conf/internal/peer"
	"github.com/user/wg-conf/internal/store"
	"github.com/user/wg-conf/internal/web"
	"github.com/user/wg-conf/internal/wireguard"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	var (
		devMode    = flag.Bool("dev", false, "use ./dev fixtures for local development")
		listenAddr = flag.String("listen", ":8080", "HTTP listen address")
		paramsPath = flag.String("params", "/etc/wireguard/params", "path to WireGuard params file")
		wgDir      = flag.String("wg-dir", "/etc/wireguard", "WireGuard config directory")
		dbPath     = flag.String("db", "/var/lib/wg-conf/wg-conf.db", "SQLite database path")
		apiKey     = flag.String("api-key", os.Getenv("WG_CONF_API_KEY"), "API key for authentication")
		clientsDir = flag.String("clients-dir", "/root", "directory with angristan client configs (wg0-client-*.conf)")
		interval   = flag.Duration("monitor-interval", 30*time.Second, "stats collection interval")
	)
	flag.Parse()

	slog.Info("wg-conf starting", "listen", *listenAddr, "params", *paramsPath)

	if *devMode {
		if err := devsetup.Ensure("dev"); err != nil {
			slog.Error("prepare dev fixtures", "error", err)
			os.Exit(1)
		}
		*paramsPath = "dev/params"
		*wgDir = "dev"
		*dbPath = "dev/wg-conf.db"
		*clientsDir = "dev"
		slog.Info("dev mode enabled", "params", *paramsPath, "wg_dir", *wgDir, "db", *dbPath)
	}

	if *apiKey == "" {
		slog.Warn("API key not set — endpoints are open; set -api-key or WG_CONF_API_KEY")
	}

	params, err := config.LoadParams(*paramsPath)
	if err != nil {
		slog.Error("load params", "error", err)
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			slog.Info("hint: for local dev run with -dev flag, on server use sudo or readable -params path")
		}
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		slog.Error("create db dir", "error", err)
		os.Exit(1)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		slog.Error("open store", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	wg, err := wireguard.New()
	if err != nil {
		slog.Error("wireguard client", "error", err)
		os.Exit(1)
	}
	defer wg.Close()

	peerSvc := peer.NewService(params, *wgDir, []string{*clientsDir}, st, wg)
	ctx := context.Background()
	if err := peerSvc.SyncFromConfig(ctx); err != nil {
		slog.Warn("initial sync from config", "error", err)
	}

	collector := monitor.New(params, *wgDir, st, wg, *interval)
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(runCtx)

	apiSrv := api.New(params, peerSvc, st, *apiKey)
	webHandler, err := web.Handler()
	if err != nil {
		slog.Error("web handler", "error", err)
		os.Exit(1)
	}

	root := chi.NewRouter()
	root.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	root.Mount("/api", apiSrv.Handler())
	root.Handle("/*", webHandler)

	server := &http.Server{
		Addr:              *listenAddr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("HTTP server listening", "addr", *listenAddr, "interface", params.ServerWGNIC, "url", "http://"+formatListenURL(*listenAddr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func formatListenURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	if strings.Count(addr, ":") == 1 && !strings.Contains(addr, "[") {
		host, port, err := net.SplitHostPort(addr)
		if err == nil && (host == "" || host == "0.0.0.0" || host == "::") {
			return "localhost:" + port
		}
	}
	return addr
}
