package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/urfave/cli/v2"

	"lamrouter/internal/config"
	"lamrouter/internal/db"
	"lamrouter/internal/handlers"
	"lamrouter/internal/handlers/routerapi"
	"lamrouter/internal/middleware"
	"lamrouter/internal/shutdown"
	"lamrouter/internal/tui"
	"lamrouter/internal/updater"
	"lamrouter/internal/webdist"
)

func init() {
	// Force IPv4 (tcp4) for all googleapis.com requests across the entire application.
	// Google CloudCode / Antigravity blocks datacenter & regional IPv6 ranges with
	// "User location is not supported for the API use".
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		origDial := t.DialContext
		t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.Contains(addr, "googleapis.com") {
				return dialer.DialContext(ctx, "tcp4", addr)
			}
			if origDial != nil {
				return origDial(ctx, network, addr)
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}
}

func main() {
	app := &cli.App{
		Name:  "lamrouter",
		Usage: "AI API proxy gateway with token saver features",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "rtk",
				Value: os.Getenv("RTK_ENABLED") != "false",
				Usage: "enable RTK input compression (env: RTK_ENABLED)",
			},
			&cli.BoolFlag{
				Name:  "caveman",
				Value: os.Getenv("CAVEMAN_ENABLED") == "true",
				Usage: "enable Caveman terse output style (env: CAVEMAN_ENABLED)",
			},
			&cli.BoolFlag{
				Name:  "ponytail",
				Value: os.Getenv("PONYTAIL_ENABLED") == "true",
				Usage: "enable Ponytail lazy dev code style (env: PONYTAIL_ENABLED)",
			},
			&cli.BoolFlag{
				Name:  "auto-update",
				Value: os.Getenv("AUTO_UPDATE") == "true",
				Usage: "automatically download and install updates if available (env: AUTO_UPDATE)",
			},
			&cli.BoolFlag{
				Name:  "no-injection-guard",
				Value: os.Getenv("INJECTION_GUARD_DISABLED") == "true",
				Usage: "disable the prompt-injection detector (on by default; env: INJECTION_GUARD_DISABLED)",
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "tui",
				Aliases: []string{"menu", "ui"},
				Usage:   "Launch interactive Charmbracelet Terminal UI dashboard",
				Action: func(cCtx *cli.Context) error {
					cfg := config.LoadConfig()
					baseURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
					return tui.Run(baseURL)
				},
			},
			{
				Name:  "version",
				Usage: "Display version details and check for updates",
				Action: func(cCtx *cli.Context) error {
					info, err := updater.CheckUpdate(cCtx.Context)
					if err != nil {
						fmt.Printf("lamrouter version %s (%s/%s)\nUpdate check failed: %v\n", updater.CurrentVersion, runtime.GOOS, runtime.GOARCH, err)
						return nil
					}
					fmt.Printf("lamrouter version %s (%s/%s)\n", info.CurrentVersion, info.OS, info.Arch)
					fmt.Printf("Latest version: %s\n", info.LatestVersion)
					if info.HasUpdate {
						fmt.Printf("\n🚀 NEW UPDATE AVAILABLE! (%s)\nNotes: %s\nRun 'lamrouter update' to install.\n", info.LatestVersion, info.ReleaseNotes)
					} else {
						fmt.Println("App is up to date.")
					}
					return nil
				},
			},
			{
				Name:  "update",
				Usage: "Check and perform self-update to the latest version",
				Action: func(cCtx *cli.Context) error {
					fmt.Printf("Checking for updates (current: %s)...\n", updater.CurrentVersion)
					info, err := updater.CheckUpdate(cCtx.Context)
					if err != nil {
						return fmt.Errorf("update check failed: %w", err)
					}
					if !info.HasUpdate {
						fmt.Printf("lamrouter is already on the latest version (%s).\n", info.CurrentVersion)
						return nil
					}
					fmt.Printf("Downloading update v%s...\n", info.LatestVersion)
					if err := updater.PerformSelfUpdate(info.DownloadURL); err != nil {
						return fmt.Errorf("update failed: %w", err)
					}
					fmt.Println("✅ lamrouter updated successfully!")
					return nil
				},
			},
			{
				Name:  "mitm",
				Usage: "Manage MITM proxy for CLI tool traffic interception",
				Subcommands: []*cli.Command{
					{
						Name:   "enable",
						Usage:  "Start MITM proxy (DNS redirect + TLS intercept on :443)",
						Action: mitmEnable,
					},
					{
						Name:   "disable",
						Usage:  "Stop MITM proxy and remove DNS entries",
						Action: mitmDisable,
					},
					{
						Name:   "status",
						Usage:  "Show MITM proxy status",
						Action: mitmStatus,
					},
				},
			},
		},
		Action: runServer,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func runServer(cCtx *cli.Context) error {
	if logPath := os.Getenv("LOG_FILE"); logPath != "" {
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			log.SetOutput(logFile)
			defer logFile.Close()
		} else {
			log.Printf("[config] warning: cannot open LOG_FILE=%s, using stderr: %v", logPath, err)
		}
	}

	cfg := config.LoadConfig()

	if err := db.InitGlobalDatabase(cfg.DatabasePath); err != nil {
		return fmt.Errorf("database init: %w", err)
	}

	conn, err := db.GetConnection()
	if err != nil {
		return fmt.Errorf("database connect: %w", err)
	}
	defer conn.Close()

	repo := db.NewRepo(conn)

	ts := handlers.NewTokenSaverConfig(cCtx.Bool("rtk"), cCtx.Bool("caveman"), cCtx.Bool("ponytail"))
	if settings, sErr := repo.GetSettings(); sErr == nil && settings != nil {
		rtk := settings.RTKEnabled
		if cCtx.IsSet("rtk") {
			rtk = cCtx.Bool("rtk")
		}
		caveman := settings.CavemanEnabled
		if cCtx.IsSet("caveman") {
			caveman = cCtx.Bool("caveman")
		}
		ponytail := settings.PonytailEnabled
		if cCtx.IsSet("ponytail") {
			ponytail = cCtx.Bool("ponytail")
		}
		ts.SetAll(rtk, caveman, ponytail)
		ts.SetCaveman(caveman, settings.CavemanLevel)
		ts.SetPonytail(ponytail, settings.PonytailLevel)
	}
	ts.SetInjectionGuard(!cCtx.Bool("no-injection-guard"))
	log.Printf("[config] token savers — rtk=%v caveman=%v (%s) ponytail=%v (%s)", ts.RTKEnabled(), ts.CavemanEnabled(), ts.CavemanLevel(), ts.PonytailEnabled(), ts.PonytailLevel())
	log.Printf("[config] prompt-injection guard enabled=%v", ts.InjectionGuardEnabled())

	updater.StartBackgroundCheck(cCtx.Bool("auto-update"))

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.MaxBody(middleware.DefaultMaxBodySize))
	r.Use(chiMiddleware.Recoverer)

	r.Use(middleware.RequestLogger)

	// Register RouterAPI v1 REST API for UI dashboard
	routerapi.RegisterV1Routes(r, repo, ts)

	// Mount standard LAM-Router AI API routes under /v1 for OpenAI/Anthropic/Media
	r.Route("/v1", func(v1 chi.Router) {
		handlers.SetupV1ProxyRoutes(v1, repo, ts)
	})

	handlers.SetupServerRouter(r, repo, ts)
	webdist.RegisterWebUI(r)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("LAM-Router (%s) starting on port %d", updater.CurrentVersion, cfg.Port)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	fmt.Fprintf(os.Stdout, "\n  🚀 LAM-Router (%s) on %s\n\n", updater.CurrentVersion, addr)
	log.Printf("Server is ready to handle requests at %s", addr)

	<-signals // first signal → begin graceful shutdown
	fmt.Fprintln(os.Stdout, "\n  Shutting down...")

	// Signal in-flight SSE streams to end promptly: the stall reader closes each
	// upstream body, handlers emit a final [DONE], and Shutdown completes well
	// within its deadline instead of waiting out the full timeout.
	shutdown.Cancel()

	// A second signal force-quits immediately (e.g. a stream stuck mid-drain).
	go func() {
		<-signals
		fmt.Fprintln(os.Stdout, "\n  Force quitting...")
		os.Exit(1)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		// Log instead of Fatalf so the deferred conn.Close()/logFile.Close()
		// still run; SQLite WAL recovers any straggler on next open.
		log.Printf("Server shutdown did not complete in time: %v", err)
	} else {
		log.Println("Server stopped gracefully")
	}
	return nil
}
