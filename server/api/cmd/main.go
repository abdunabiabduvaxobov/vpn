package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"vpnapp/server/api/internal/bot"
	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/repository"
	"vpnapp/server/api/internal/scheduler"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/utils"
	stripe "github.com/stripe/stripe-go/v81"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Fail-fast aggregate env validator (HOTFIX-08 / D-04). Runs BEFORE
	// config.Load() so the operator sees ALL missing required keys in one
	// structured log line rather than fixing them one at a time. logger.Fatal
	// calls os.Exit(1) internally — satisfies the D-04 single-pass contract.
	if missing := config.RequireEnv(); len(missing) > 0 {
		logger.Fatal("required environment variables missing",
			zap.Strings("missing", missing),
			zap.String("action", "set the listed variables and restart"),
		)
	}

	// Load configuration from environment
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Optional payment-provider env vars (HOTFIX-08 / D-03). Empty or
	// placeholder Stripe values emit a single WARN line but do NOT block
	// startup — Stripe leaves in Phase 8 and these will shrink to zero then.
	// LAVA_* will be added to RequireEnv in Phase 3 when lava.top wires in.
	if warned := config.OptionalEnvWarnings(); len(warned) > 0 {
		logger.Warn("optional payment-provider environment variables unset or placeholder",
			zap.Strings("vars", warned),
			zap.String("impact", "stripe checkout will fail at runtime; lava.top not yet wired"),
		)
	}

	// Set Stripe API key once at startup — handlers must not override this.
	stripe.Key = cfg.StripeKey

	// Connect to PostgreSQL
	db, err := repository.NewDB(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	logger.Info("connected to database")

	// Connect to Redis.
	redisClient, err := cache.NewRedisClient(cfg.RedisURL)
	if err != nil {
		logger.Fatal("failed to connect to redis", zap.Error(err))
	}
	logger.Info("connected to redis")

	// Start background session cleanup scheduler.
	scheduler.Start(db, logger, cfg)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:      "VPN API Server",
		ServerHeader: "",
		ErrorHandler: handler.ErrorHandler(logger),
	})

	// Global middleware.
	//
	// requestid MUST run BEFORE recover.New() so panic-recovery paths also
	// carry a request_id that ErrorHandler can echo to the client and stamp
	// onto the structured log line (HOTFIX-04 / D-05). UUIDv4 (RFC 4122
	// random) is chosen over the default utils.UUID — the latter is faster
	// but monotonic, leaking request count to anyone watching responses.
	app.Use(requestid.New(requestid.Config{
		Header:    fiber.HeaderXRequestID,
		Generator: utils.UUIDv4,
	}))
	app.Use(recover.New())
	// CORS is pinned to the admin panel origin now that it lives on its
	// own subdomain. The port is load-bearing: browsers include :9443 in
	// the Origin header because 9443 is a non-default HTTPS port, and
	// Fiber does exact string matching against this list. Omitting the
	// port silently breaks preflight (no access-control-allow-origin
	// header in the response, which the browser blocks without a clear
	// error in the network tab).
	//
	// The mobile client is a React Native app and does not send an
	// Origin header, so it is unaffected by this allow-list. If a second
	// admin origin is added later (e.g. a staging domain), comma-separate
	// it here — Fiber supports multiple exact origins.
	app.Use(cors.New(cors.Config{
		AllowOrigins: "https://vpnadmin.mydayai.uz:9443",
		AllowHeaders: "Origin, Content-Type, Authorization, X-App-Version",
	}))

	// App version gate — rejects mobile clients below MIN_APP_VERSION.
	// Bypasses are scoped to (method, path) so that for example
	// POST /health is still gated even though GET /health is not.
	app.Use(middleware.AppVersion(
		cfg.MinAppVersion,
		logger,
		middleware.SkipRule{Method: fiber.MethodGet, Path: "/api/v1/health"},
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/webhook/stripe"},
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/auth/admin-login"},
		// The web admin panel does not send X-App-Version. Both the refresh
		// endpoint (called whenever the 5-min access token expires) and the
		// entire /admin/ route tree must bypass the mobile version gate.
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/auth/refresh"},
		middleware.SkipRule{Path: "/api/v1/admin/", Prefix: true},
	))

	// Redis-backed per-user rate limiting. Decodes the JWT (when present) to
	// key on user ID so the limit applies correctly even before auth middleware runs.
	app.Use(middleware.RateLimit(redisClient, logger, cfg.JWTSecret))

	// API routes
	api := app.Group("/api/v1")

	// Public routes (no auth required)
	api.Post("/auth/refresh", handler.RefreshToken(logger, cfg, db))
	api.Post("/auth/guest", handler.GuestLogin(logger, db, cfg))
	api.Post("/auth/admin-login", handler.AdminLogin(logger, cfg, db))
	// /auth/link is intentionally public — the calling device is a brand-new
	// guest that does not yet hold a token for the target account it wants
	// to attach to via the share code. The dedicated rate limiter caps
	// brute-force attempts at 10/minute/IP independently of the global
	// 30/minute/IP bucket so the link endpoint cannot be amortised over
	// other public endpoints.
	api.Post("/auth/link",
		middleware.LinkAttemptLimit(redisClient, logger),
		handler.LinkDevice(logger, cfg, db),
	)
	api.Get("/health", handler.Health())

	// Stripe webhook — public route, authenticated via Stripe-Signature header.
	api.Post("/webhook/stripe", handler.HandleStripeWebhook(logger, cfg, db))

	// Debug endpoint — logs only the "error" and "action" fields from
	// client-side error reports. The body is intentionally NOT logged in
	// full because clients can include sensitive material (device_id,
	// device_secret, tokens) and the request body would otherwise leak
	// into log aggregation. Anything else needed for diagnosis should be
	// added as an explicit field on the client side.
	api.Post("/debug/error", func(c *fiber.Ctx) error {
		var body struct {
			Error  string `json:"error"`
			Action string `json:"action"`
		}
		if err := c.BodyParser(&body); err == nil {
			logger.Warn("CLIENT ERROR REPORT",
				zap.String("error", body.Error),
				zap.String("action", body.Action),
			)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	// Protected routes (JWT required)
	authMiddleware := middleware.AuthRequired(cfg.JWTSecret, redisClient, db)
	protected := api.Group("", authMiddleware)
	protected.Get("/servers", handler.ListServers(logger, db))
	protected.Get("/servers/:id/config", handler.GetServerConfig(logger, db, cfg))
	protected.Get("/subscription", handler.GetSubscription(logger, db))
	protected.Get("/account", handler.GetAccount(logger, db))
	protected.Patch("/account", handler.PatchAccount(logger, db))
	protected.Post("/connections", handler.RegisterConnection(logger, db))
	protected.Delete("/connections/:id", handler.UnregisterConnection(logger, db))
	protected.Get("/connections", handler.ListActiveConnections(logger, db))
	protected.Patch("/connections/:id/heartbeat", handler.HeartbeatConnection(logger, db))
	protected.Post("/subscription/checkout", handler.CreateCheckoutSession(logger, cfg, db))
	protected.Post("/subscription/cancel", handler.CancelSubscription(logger, cfg, db))
	// Plan sharing — owner generates a code, friend's device redeems it via /auth/link.
	protected.Post("/devices/share-code", handler.CreateShareCode(logger, cfg, db))
	protected.Get("/devices", handler.ListMyDevices(logger, db))
	protected.Delete("/devices/:id", handler.DeleteMyDevice(logger, db))

	// Telegram recovery (ADR-006). Both intent endpoints are
	// authenticated (the mobile client calls them with its current
	// JWT) and return short-lived signed URLs that open the bot's
	// /start handler. The status endpoint tells the Account screen
	// whether the user is already linked; the unlink endpoint
	// clears the binding so the user can re-link to a different
	// Telegram account.
	protected.Post("/auth/telegram/link-intent", handler.TelegramLinkIntent(logger, redisClient))
	protected.Post("/auth/telegram/restore-intent", handler.TelegramRestoreIntent(logger, redisClient))
	protected.Get("/account/telegram-status", handler.TelegramStatus(logger, db))
	protected.Delete("/account/telegram", handler.TelegramUnlink(logger, db))

	// Admin routes (JWT + admin role required).
	// The audit middleware wraps the whole group so every mutating
	// admin action is persisted to the audit_log table on success.
	admin := api.Group("/admin",
		authMiddleware,
		middleware.AdminRequired(db),
		middleware.AuditLog(db, logger),
	)
	admin.Get("/users", handler.AdminListUsers(logger, db))
	admin.Get("/users/:id", handler.AdminGetUser(logger, db))
	admin.Patch("/users/:id", handler.AdminUpdateUser(logger, db))
	admin.Get("/servers", handler.AdminListServers(logger, db))
	admin.Post("/servers", handler.AdminCreateServer(logger, db))
	admin.Patch("/servers/:id", handler.AdminUpdateServer(logger, db))
	admin.Delete("/servers/:id", handler.AdminDeleteServer(logger, db))
	admin.Get("/stats", handler.AdminGetStats(logger, db))
	admin.Get("/stats/timeseries", handler.AdminGetStatsTimeseries(logger, db))
	admin.Get("/analytics", handler.AdminGetAnalytics(logger, db))
	admin.Get("/users/:id/devices", handler.AdminListUserDevices(logger, db))
	admin.Delete("/users/:id/devices/:device_id", handler.AdminDeleteUserDevice(logger, db))
	admin.Get("/users/:id/connections", handler.AdminListUserConnections(logger, db))
	admin.Get("/audit-log", handler.AdminGetAuditLog(logger, db))
	// Mounted under /admin so it's covered by the version-gate prefix
	// skip, the AdminRequired middleware, and the audit middleware
	// automatically. Full path: /api/v1/admin/change-password.
	admin.Post("/change-password", handler.AdminChangePassword(logger, db))

	// Telegram recovery bot (ADR-006). Starts only when the env var
	// TELEGRAM_RECOVERY_BOT_TOKEN is set — deployments without it
	// run fine and just don't expose the recovery flow. Runs for
	// the lifetime of the process, cancelled via botCtx on shutdown.
	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()
	recoveryBot, err := bot.New(cfg, db, redisClient, logger)
	if err != nil {
		logger.Fatal("failed to initialise telegram recovery bot", zap.Error(err))
	}
	if recoveryBot != nil {
		// Propagate the live bot username back to the handlers so
		// the deep links they mint match the actual bot.
		handler.SetTelegramBotUsername(cfg.RecoveryBotUsername)
		go recoveryBot.Run(botCtx)
	} else {
		logger.Info("telegram recovery bot disabled (no TELEGRAM_RECOVERY_BOT_TOKEN set)")
	}

	// Start server
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Port)
		logger.Info("starting API server", zap.String("addr", addr))
		if err := app.Listen(addr); err != nil {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	botCancel()

	logger.Info("shutting down API server...")

	// Stop accepting new connections first.
	if err := app.Shutdown(); err != nil {
		logger.Error("error during server shutdown", zap.Error(err))
	}

	// Stop the background scheduler.
	scheduler.Stop()
	logger.Info("scheduler stopped")

	// Close database connection.
	sqlDB, err := db.DB()
	if err == nil {
		if err := sqlDB.Close(); err != nil {
			logger.Error("error closing database", zap.Error(err))
		}
	}
	logger.Info("database connection closed")

	// Close Redis connection.
	if err := redisClient.Close(); err != nil {
		logger.Error("error closing redis", zap.Error(err))
	}
	logger.Info("redis connection closed")
}
