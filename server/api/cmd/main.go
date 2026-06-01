package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vpnapp/server/api/internal/auth/apple"
	"vpnapp/server/api/internal/auth/google"
	"vpnapp/server/api/internal/bot"
	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/repository"
	"vpnapp/server/api/internal/scheduler"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/utils"
	"go.uber.org/zap"
)

// buildFiberConfig assembles the Fiber server configuration. It is extracted
// from main() so the PERF-09 / D-09c hardening values can be unit-tested
// (TestServerConfig in server_config_test.go) without booting the full app.
//
// The four timeout/limit fields are the hardening this phase adds:
//   - BodyLimit caps oversized-body memory pressure (T-06-BODYDOS).
//   - ReadTimeout + IdleTimeout close slow/idle clients (T-06-SLOWLORIS,
//     audit §6.2).
//   - WriteTimeout bounds slow-consumer write stalls.
//
// errorHandler is injected because it closes over the request logger
// (constructed in main); the rest of the config is static.
func buildFiberConfig(errorHandler fiber.ErrorHandler) fiber.Config {
	return fiber.Config{
		AppName:                 "VPN API Server",
		ServerHeader:            "",
		ErrorHandler:            errorHandler,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{},
		BodyLimit:               64 * 1024,         // PERF-09 / D-09c — caps request-body DoS surface
		ReadTimeout:             15 * time.Second,  // PERF-09 / D-09c — slowloris defense
		WriteTimeout:            30 * time.Second,  // PERF-09 / D-09c
		IdleTimeout:             120 * time.Second, // audit §6.2 — close idle keepalive sockets
		// Prefork left false (default): prefork forks separate processes, each
		// with its OWN DB pool — that breaks the shared, tuned pool below.
	}
}

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

	// Tunable env vars that were set but failed to parse (e.g.
	// STALE_CONNECTION_AFTER=3min should be 3m). The helpers fell back
	// to their compiled-in default; without this log line the operator
	// would never know their tuning intent was discarded. See WR-05.
	if len(cfg.EnvParseWarnings) > 0 {
		logger.Warn("tunable environment variables failed to parse — falling back to defaults",
			zap.Strings("offenders", cfg.EnvParseWarnings),
		)
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

	// Phase 2 SSO verifier construction (D-34 — once at startup, DI into
	// handlers). The Apple verifier launches a non-blocking JWKs refresh
	// goroutine; the Google verifier has no init-time work. Audience
	// whitelists are sourced from the HOTFIX-08 required env vars
	// validated above.
	appleVerifier, err := apple.New(apple.Options{
		AllowedAudiences: []string{cfg.AppleBundleID, cfg.AppleServiceID},
		AllowedIssuer:    "https://appleid.apple.com",
	})
	if err != nil {
		logger.Fatal("apple verifier init", zap.Error(err))
	}
	googleVerifier := google.New([]string{
		cfg.GoogleClientIDIOS,
		cfg.GoogleClientIDAndroid,
		cfg.GoogleClientIDWeb,
	})

	// Phase 3 lava.top client (D-14). Constructed once at startup with the
	// active API key (selected by LAVA_ENV — config.go resolves into LavaActiveAPIKey).
	// SSRF mitigation: BaseURL is hardcoded in internal/lava/client.go (D-15);
	// API key never logged or echoed in error paths (D-32 §3).
	lavaClient := lava.New(cfg.LavaActiveAPIKey)
	logger.Info("lava client constructed", zap.String("env", cfg.LavaEnv))

	// Parse LAVA_WEBHOOK_ALLOWED_CIDRS and construct the route-scoped IP allowlist
	// middleware. Per RESEARCH §2.1+§2.2, Fiber's EnableTrustedProxyCheck alone
	// does NOT reject untrusted IPs — it only ignores their X-Forwarded-* headers
	// and falls back to RemoteIP(). This dedicated middleware reads
	// c.Context().RemoteIP() (TCP-layer) and 403s on mismatch (PAY-06).
	lavaCIDRs := strings.Split(cfg.LavaWebhookAllowedCIDRs, ",")
	lavaIPAllowlist, err := middleware.LavaWebhookIPAllowlist(lavaCIDRs, logger)
	if err != nil {
		logger.Fatal("LAVA_WEBHOOK_ALLOWED_CIDRS parse failed", zap.Error(err))
	}

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
	// PERF-04 / D-06: redisClient lets the bulk-downgrade passes bust
	// user:<id> for each downgraded user (zero-lag entitlement freshness).
	//
	// PERF-06 / D-09b: gate the scheduler behind RUN_SCHEDULER (cfg.RunScheduler
	// is ShouldRunScheduler(getEnv("RUN_SCHEDULER","true")) computed in Load()).
	// Default true → single-replica v2.2.0 keeps running periodic jobs. Set
	// RUN_SCHEDULER=false on non-primary replicas (Tranche 3 multi-replica) so
	// only one replica fires the cleanup / 10s heartbeat flush / weekly prune.
	if cfg.RunScheduler {
		scheduler.Start(db, logger, cfg, redisClient)
	} else {
		logger.Info("scheduler disabled (RUN_SCHEDULER=false) — this replica runs no periodic jobs")
	}

	// Create Fiber app.
	//
	// CR-01 fix: TrustedProxies enumerates the L7 intermediaries (reverse proxy,
	// CDN, load balancer) whose X-Forwarded-* headers we honour for c.IP(). lava
	// is a third-party HTTP CLIENT (webhook origin), NOT a trusted L7 proxy, so
	// its CIDR set must NOT appear here — otherwise lava could spoof c.IP() via
	// X-Forwarded-For for the rate limiter, audit log, and any other code path
	// keyed on c.IP().
	//
	// For the v2.2.0 single-VM Docker Compose deploy there is no L7 proxy in
	// front of the API container — TrustedProxies is therefore an empty slice
	// ("trust nothing"). When a reverse proxy is introduced later, populate this
	// from a NEW config field (e.g. cfg.TrustedProxyCIDRs), NOT from
	// LAVA_WEBHOOK_ALLOWED_CIDRS. The webhook route still gets its own
	// LavaWebhookIPAllowlist middleware on top — that one correctly uses
	// c.Context().RemoteIP() (TCP-layer, unspoofable) per PAY-06.
	app := fiber.New(buildFiberConfig(handler.ErrorHandler(logger)))

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
		// Phase 3 public plans (PAY-12). Landing-site browsers GET /plans
		// directly and don't send X-App-Version. No auth on this route.
		middleware.SkipRule{Method: fiber.MethodGet, Path: "/api/v1/plans"},
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/auth/admin-login"},
		// The web admin panel does not send X-App-Version. Both the refresh
		// endpoint (called whenever the 5-min access token expires) and the
		// entire /admin/ route tree must bypass the mobile version gate.
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/auth/refresh"},
		// lava.top server posts the webhook without an X-App-Version header.
		// IP allowlist + X-Api-Key are the auth gates for this route.
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/webhook/lava"},
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
	// Phase 2 SSO endpoints (D-26). Public — optionally read Authorization
	// for guest-promotion intent (D-06). Logout (AUTH-08) is mounted under
	// the protected group below.
	api.Post("/auth/apple", handler.AppleSignIn(logger, cfg, db, appleVerifier))
	api.Post("/auth/google", handler.GoogleSignIn(logger, cfg, db, googleVerifier))
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

	// Phase 3 public plans endpoint (PAY-12). No auth — landing /pricing reads
	// this. Cached in Redis (cache:plans:public:{currency}, TTL 60s); admin
	// writes (plan 03-08) bust via cache.BustPlansCache after a successful
	// mutation. Currency derived from ?currency=USD|EUR|RUB or Accept-Language
	// (D-27); response excludes admin-only fields per D-27.
	api.Get("/plans", handler.ListPlansPublic(logger, db, redisClient))

	// Phase 3 lava webhook (PAY-03..09). PUBLIC route — auth is via:
	//   1. LavaWebhookIPAllowlist (TCP-layer RemoteIP check, 403 on miss).
	//   2. X-Api-Key header inside the handler (crypto/subtle.ConstantTimeCompare).
	// The old Stripe webhook route has been removed (D-02).
	api.Post("/webhook/lava", lavaIPAllowlist, handler.HandleLavaWebhook(logger, cfg, db, lavaClient, redisClient))

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
	// Phase 2 Logout (AUTH-08, D-26). Mounted under the protected group so
	// AuthRequired validates the JWT and sets c.Locals("user_id") before
	// the handler runs. The middleware also already checks
	// cache.IsTokenBlacklisted on every request — once Logout writes the
	// token's SHA-256 hash to the blacklist (via cache.BlacklistToken with
	// the in-tree "token:blacklist:" prefix), subsequent requests with the
	// same access token return 401 automatically.
	protected.Post("/auth/logout", handler.Logout(logger, redisClient, db))
	protected.Get("/servers", handler.ListServersCached(logger, db, redisClient))
	protected.Get("/servers/:id/config", handler.GetServerConfig(logger, db, cfg))
	protected.Get("/subscription", handler.GetSubscription(logger, db))
	protected.Get("/account", handler.GetAccount(logger, db))
	protected.Patch("/account", handler.PatchAccount(logger, db))
	protected.Post("/connections", handler.RegisterConnection(logger, db))
	protected.Delete("/connections/:id", handler.UnregisterConnection(logger, db))
	protected.Get("/connections", handler.ListActiveConnections(logger, db))
	protected.Patch("/connections/:id/heartbeat", handler.HeartbeatConnection(logger, db, redisClient))
	// Phase 3 lava endpoints (D-02). /checkout supersedes the legacy
	// Stripe-era subscription/checkout path.
	// All three are PROTECTED (JWT required) — guest users get 403 from the handler
	// itself (CreateCheckoutSession checks user.Email presence).
	protected.Post("/checkout", handler.CreateCheckoutSession(logger, cfg, db, lavaClient))
	protected.Post("/subscription/cancel", handler.CancelSubscription(logger, cfg, db, lavaClient))
	protected.Get("/invoices/:id", handler.GetInvoice(logger, cfg, db, lavaClient))
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
	admin.Patch("/users/:id", handler.AdminUpdateUser(logger, db, redisClient))
	admin.Get("/servers", handler.AdminListServers(logger, db))
	admin.Post("/servers", handler.AdminCreateServer(logger, db, redisClient))
	admin.Patch("/servers/:id", handler.AdminUpdateServer(logger, db, redisClient))
	admin.Delete("/servers/:id", handler.AdminDeleteServer(logger, db, redisClient))
	admin.Get("/stats", handler.AdminGetStats(logger, db, redisClient))
	admin.Get("/stats/timeseries", handler.AdminGetStatsTimeseries(logger, db))
	admin.Get("/analytics", handler.AdminGetAnalytics(logger, db))
	admin.Get("/users/:id/devices", handler.AdminListUserDevices(logger, db))
	admin.Delete("/users/:id/devices/:device_id", handler.AdminDeleteUserDevice(logger, db))
	admin.Get("/users/:id/connections", handler.AdminListUserConnections(logger, db))
	admin.Get("/audit-log", handler.AdminGetAuditLog(logger, db))
	// Phase 3 lava admin endpoint (D-12 Option B). Proxies /api/v2/products
	// via server-side API key so admin can pick lava offers from a dropdown.
	// Inherits AuthRequired + AdminRequired + AuditLog from the admin group.
	admin.Get("/lava/products", handler.AdminListLavaProducts(logger, lavaClient))
	// Phase 3 admin plans CRUD (PAY-13/14/15). All routes inherit
	// AuthRequired + AdminRequired + AuditLog from the admin group.
	// Every write handler busts cache:plans:public:* via cache.BustPlansCache
	// so the public /pricing page reflects admin edits within one HTTP cycle
	// (the 60s TTL is the bounded fallback if the bust DEL fails).
	// describeAction extension in middleware/audit.go labels these
	// with create_plan / replace_plan_offer / etc.
	admin.Get("/plans", handler.AdminListPlans(logger, db))
	admin.Post("/plans", handler.AdminCreatePlan(logger, db, redisClient))
	admin.Get("/plans/:id", handler.AdminGetPlan(logger, db))
	admin.Patch("/plans/:id", handler.AdminUpdatePlan(logger, db, redisClient))
	admin.Delete("/plans/:id", handler.AdminDeletePlan(logger, db, redisClient))
	admin.Put("/plans/:id/servers", handler.AdminReplacePlanServers(logger, db, redisClient))
	admin.Post("/plans/:id/servers/:server_id", handler.AdminAddPlanServer(logger, db, redisClient))
	admin.Delete("/plans/:id/servers/:server_id", handler.AdminRemovePlanServer(logger, db, redisClient))
	admin.Get("/plans/:id/offers", handler.AdminListPlanOffers(logger, db))
	admin.Post("/plans/:id/offers", handler.AdminCreatePlanOffer(logger, db, redisClient))
	admin.Patch("/plans/:id/offers/:offer_id", handler.AdminUpdatePlanOffer(logger, db, redisClient))
	admin.Delete("/plans/:id/offers/:offer_id", handler.AdminDeletePlanOffer(logger, db, redisClient))
	admin.Post("/plans/:id/offers/:offer_id/replace", handler.AdminReplacePlanOffer(logger, db, redisClient))
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
