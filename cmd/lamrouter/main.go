package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	"lamrouter/internal/handlers/chat"
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
				Name:  "update-check",
				Value: os.Getenv("UPDATE_CHECK") != "false",
				Usage: "check npm for a newer release shortly after startup and log it (env: UPDATE_CHECK=false to disable)",
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
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "url",
						Aliases: []string{"base-url", "u"},
						Usage:   "Target router base URL (e.g. https://lam-router.buatjalan.xyz)",
					},
				},
				Action: func(cCtx *cli.Context) error {
					cfg := config.LoadConfig()
					baseURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
					if u := cCtx.String("url"); u != "" {
						baseURL = strings.TrimRight(u, "/")
					} else if cCtx.NArg() > 0 && strings.HasPrefix(cCtx.Args().First(), "http") {
						baseURL = strings.TrimRight(cCtx.Args().First(), "/")
					} else if envU := os.Getenv("LAM_ROUTER_URL"); envU != "" {
						baseURL = strings.TrimRight(envU, "/")
					}

					// Auto-start embedded background router if not already running (only when connecting to localhost)
					if (strings.Contains(baseURL, "127.0.0.1") || strings.Contains(baseURL, "localhost")) && !isServerHealthy(baseURL) {
						cleanup, err := startEmbeddedServer(cCtx, cfg)
						if err != nil {
							return fmt.Errorf("auto-start server for TUI: %w", err)
						}
						defer cleanup()
					}

					return tui.Run(baseURL)
				},
			},
			{
				Name:  "version",
				Usage: "Show the running version and check npm for a newer release",
				Action: func(cCtx *cli.Context) error {
					channel, path := updater.DetectInstallChannel()
					fmt.Printf("lamrouter version %s (%s/%s)\n", updater.CurrentVersion, runtime.GOOS, runtime.GOARCH)
					fmt.Printf("Install channel: %s\n", channel)
					if path != "" {
						fmt.Printf("Binary path:     %s\n", path)
					}

					info, err := updater.CheckUpdate(cCtx.Context)
					if err != nil {
						fmt.Printf("Latest on npm:   unavailable (%v)\n", err)
						return nil
					}
					fmt.Printf("Latest on npm:   %s\n", info.LatestVersion)
					if info.HasUpdate {
						fmt.Printf("\n🚀 UPDATE AVAILABLE: %s → %s\nUpdate:  %s\nRestart: %s\n", info.CurrentVersion, info.LatestVersion, info.UpdateCommand, info.RestartHint)
						if info.ReleaseURL != "" {
							fmt.Printf("Release: %s\n", info.ReleaseURL)
						}
					} else {
						fmt.Printf("\n✅ Up to date.\n")
					}
					return nil
				},
			},
			{
				Name:   "update",
				Usage:  "Install the newest release (delegates to npm for npm installs)",
				Action: runUpdate,
			}, {
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
	repo.SetJWTSecret(cfg.JWTSecret)

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

	updater.StartBackgroundCheck(cCtx.Bool("update-check"))

	r := setupRouter(cfg, repo, ts)

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

func setupRouter(cfg *config.Config, repo *db.Repo, ts *handlers.TokenSaverConfig) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.MaxBody(middleware.DefaultMaxBodySize))
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.RequestLogger)

	// Register RouterAPI v1 REST API for UI dashboard
	apiH := routerapi.RegisterV1Routes(r, repo, ts)
	chatH := chat.NewChatHandler(repo, ts)
	apiH.SetChatHandler(chatH.HandleChatCompletions)

	// Mount standard LAM-Router AI API routes under /v1 for OpenAI/Anthropic/Media
	r.Route("/v1", func(v1 chi.Router) {
		handlers.SetupV1ProxyRoutes(v1, repo, ts)
	})

	handlers.SetupServerRouter(r, repo, ts)
	webdist.RegisterWebUI(r)
	return r
}

func isServerHealthy(baseURL string) bool {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get(baseURL + "/health")
	if err == nil && resp != nil {
		resp.Body.Close()
		return resp.StatusCode < 400
	}
	return false
}

func startEmbeddedServer(cCtx *cli.Context, cfg *config.Config) (func(), error) {
	// Redirect logging to file so it doesn't garble Charmbracelet TUI terminal screen
	logDir := filepath.Join(config.ResolveDataDir(), "logs")
	_ = os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	} else {
		log.SetOutput(io.Discard)
	}

	if err := db.InitGlobalDatabase(cfg.DatabasePath); err != nil {
		return nil, fmt.Errorf("database init: %w", err)
	}

	conn, err := db.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("database connect: %w", err)
	}

	repo := db.NewRepo(conn)
	repo.SetJWTSecret(cfg.JWTSecret)
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

	r := setupRouter(cfg, repo, ts)

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	// Wait for server to be ready
	baseURL := fmt.Sprintf("http://%s", addr)
	ready := false
	for i := 0; i < 30; i++ {
		select {
		case err := <-serverErrCh:
			conn.Close()
			if logFile != nil {
				logFile.Close()
			}
			return nil, fmt.Errorf("embedded server failed to start: %w", err)
		default:
		}
		if isServerHealthy(baseURL) {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !ready {
		_ = server.Close()
		conn.Close()
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("embedded server did not respond on %s within 1.5s", baseURL)
	}

	cleanup := func() {
		shutdown.Cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		conn.Close()
		if logFile != nil {
			logFile.Close()
		}
	}

	return cleanup, nil
}

// runUpdate installs the newest release. For an npm-owned binary it delegates to
// npm and never writes inside node_modules itself; a manually installed binary
// gets instructions instead, because guessing how a binary was placed on disk is
// how you end up replacing a running gateway with the wrong file.
func runUpdate(cCtx *cli.Context) error {
	channel, path := updater.DetectInstallChannel()
	fmt.Printf("Current version: %s (%s)\n", updater.CurrentVersion, channel)
	if path != "" {
		fmt.Printf("Binary path:     %s\n", path)
	}

	info, err := updater.CheckUpdate(cCtx.Context)
	if err != nil {
		return fmt.Errorf("npm version check failed: %w", err)
	}
	fmt.Printf("Latest on npm:   %s\n", info.LatestVersion)

	if !info.HasUpdate {
		fmt.Printf("\n✅ Already on the latest release (%s). Nothing to do.\n", info.CurrentVersion)
		return nil
	}

	if channel != updater.ChannelNPM {
		fmt.Printf("\n⚠️  This binary was not installed by npm, so `npm i -g` would not replace it.\n")
		fmt.Printf("    Update it the way it was installed:\n")
		fmt.Printf("      npm install:  %s\n", updater.NPMUpdateCommand(""))
		fmt.Printf("      source build: git pull --ff-only && make install\n")
		fmt.Printf("    Then restart the service: %s\n", updater.RestartHint)
		return nil
	}

	fmt.Printf("\n⬇️  Installing lam-router %s via npm...\n", info.LatestVersion)
	if err := updater.RunNPMUpdate(info.LatestVersion); err != nil {
		return err
	}
	fmt.Printf("\n✅ Installed %s. Restart the service to run it: %s\n", info.LatestVersion, updater.RestartHint)
	return nil
}
