package handler

import (
	"encoding/json"
	"errors"
	"math/rand"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// realityServerNames is a pool of SNI domains for REALITY connections.
// Russian popular sites are included so the TLS fingerprint looks natural
// to TSPU when probed from within Russia.
// Keep in sync with server/tunnel/config.example.json "server_names".
var realityServerNames = []string{
	"www.microsoft.com",
	"microsoft.com",
	"yandex.ru",
	"www.yandex.ru",
	"mail.ru",
	"www.mail.ru",
	"vk.com",
	"www.vk.com",
	"ok.ru",
	"www.ok.ru",
	"sberbank.ru",
	"www.sberbank.ru",
	"gosuslugi.ru",
	"www.gosuslugi.ru",
}

// ServerConfig is the connection configuration sent to clients.
type ServerConfig struct {
	ServerAddress string               `json:"server_address"`
	ServerPort    int                  `json:"server_port"`
	Protocol      string               `json:"protocol"`
	UserID        string               `json:"user_id"`
	Reality       *RealityClientConfig `json:"reality,omitempty"`
	// WebSocket is present only when the server has WebSocket CDN transport configured.
	// Clients should use protocol "vless-ws" with this config when available and when
	// direct REALITY connections fail.
	WebSocket *WebSocketClientConfig `json:"websocket,omitempty"`
	// AWG is present only when the server has AmneziaWG configured (awg_public_key IS NOT NULL).
	// When present, the client may connect via protocol "amneziawg" instead of VLESS.
	// AmneziaWG is a WireGuard variant with anti-DPI obfuscation — it does not use xray-core.
	AWG *AWGClientConfig `json:"awg,omitempty"`
	// ProtocolPriority is the recommended order of protocols for this client.
	// Computed from the client's region — Russian users get WebSocket first (CDN bypass),
	// while others get REALITY first (lower latency).
	ProtocolPriority []string `json:"protocol_priority,omitempty"`
}

// AWGClientConfig holds everything the client needs to create an AmneziaWG tunnel.
// It is embedded in the ServerConfig response when the server supports AmneziaWG.
type AWGClientConfig struct {
	// PublicKey is the server's X25519 public key (Base64, 44 chars).
	PublicKey string `json:"public_key"`
	// Endpoint is the UDP address to connect to, e.g. "95.216.1.1:51820".
	Endpoint string `json:"endpoint"`
	// AllowedIPs is the set of IP ranges to route through the tunnel.
	// The client should use "0.0.0.0/0, ::/0" for a full-tunnel VPN.
	AllowedIPs string `json:"allowed_ips"`
	// Obfuscation parameters — must be mirrored on client and server.
	Jc   int `json:"jc"`
	Jmin int `json:"jmin"`
	Jmax int `json:"jmax"`
	S1   int `json:"s1"`
	S2   int `json:"s2"`
	H1   int `json:"h1"`
	H2   int `json:"h2"`
	H3   int `json:"h3"`
	H4   int `json:"h4"`
}

// RealityClientConfig holds REALITY settings for the client.
type RealityClientConfig struct {
	PublicKey   string `json:"public_key"`
	ShortID     string `json:"short_id"`
	ServerName  string `json:"server_name"`
	Fingerprint string `json:"fingerprint"`
}

// WebSocketClientConfig holds WebSocket CDN transport settings for the client.
// The client connects to Host:443 over WebSocket+TLS through Cloudflare CDN.
type WebSocketClientConfig struct {
	// Host is the Cloudflare-proxied CDN domain, e.g. "vpn.example.com".
	Host string `json:"host"`
	// Path is the WebSocket upgrade path, e.g. "/ws".
	Path string `json:"path"`
}

// ListServersCached handles GET /servers (PERF-01 / D-05).
//
// Returns active VPN servers limited by the user's plan. Admins bypass the plan
// filter and see all active servers (PAY-11, D-21).
//
// Cache: the FULL active-server list (repository.ListActiveServers) is cached as
// a single global blob `cache:servers:active` (TTL 60s). On a cache hit the heavy
// ListActiveServers SELECT is skipped entirely for BOTH paths:
//   - admin       → returns the cached blob as-is (zero servers SELECT, D-09 (b)).
//   - non-admin   → resolves the plan's allowed server IDs and INTERSECTS them
//     with the cached blob IN GO (the entitlement filter stays server-side and
//     per-request — T-06-SRVCACHE-01). The small ListServersForPlan JOIN is the
//     residual cost Fork 3 accepted (sub-ms indexed); the cached blob is what
//     removes the ~833 q/s amplifier (audit §5.2).
//
// Fail-open: a Redis outage makes GetServersCache return "" → the handler falls
// through to the DB (T-06-SRVCACHE-03). /servers never breaks.
//
// NOTE on the symbol name: the Wave 0 RED test (servers_cache_test.go,
// TestServersCacheNoSelect) pins this handler to `ListServersCached` with the
// (logger, db, redisClient) signature — see SUMMARY deviation [Rule 3]. The old
// non-cached ListServers(logger, db) is removed to avoid a dead duplicate.
func ListServersCached(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		role, _ := c.Locals("role").(string)

		// Resolve the FULL active-server list, cache-first. On a hit this emits
		// ZERO ListActiveServers SELECT (D-09 (b)); on a miss/outage it loads the
		// DB and best-effort populates the cache.
		fullServers, err := loadActiveServersCached(c, db, redisClient, logger)
		if err != nil {
			logger.Error("failed to list servers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Apply the plan filter LIVE IN GO (D-05). The cached blob is the full
		// list; admins get it unfiltered, non-admins get the plan-intersected
		// subset. The filter NEVER comes from cache — it runs per request so a
		// non-admin can never receive servers outside their plan.
		var servers []model.VPNServer
		if role == "admin" {
			// Admin bypass — sees all active servers regardless of plan.
			servers = fullServers
		} else {
			planID, perr := resolveUserPlanID(c, db)
			if perr != nil {
				logger.Error("failed to resolve plan_id for ListServers",
					zap.String("user_id", userID), zap.Error(perr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			// Build the allowed-ID set from the live, sub-ms indexed JOIN
			// (Fork 3: do NOT cache plan_servers membership), then intersect it
			// with the cached full blob so the cache win benefits non-admins too.
			allowed, perr := repository.ListServersForPlan(c.Context(), db, planID)
			if perr != nil {
				logger.Error("failed to list servers for plan", zap.Error(perr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			allowedIDs := make(map[string]struct{}, len(allowed))
			for _, s := range allowed {
				allowedIDs[s.ID] = struct{}{}
			}
			// Preserve the cached blob's current_load ordering by iterating it.
			servers = make([]model.VPNServer, 0, len(allowedIDs))
			for _, s := range fullServers {
				if _, ok := allowedIDs[s.ID]; ok {
					servers = append(servers, s)
				}
			}
		}

		logger.Debug("listing servers",
			zap.String("user_id", userID),
			zap.String("role", role),
			zap.Int("count", len(servers)),
		)
		return c.JSON(fiber.Map{
			"data": servers,
		})
	}
}

// loadActiveServersCached returns the full active-server list, reading the
// cache:servers:active blob first and falling through to ListActiveServers on a
// miss / Redis outage. On a DB load it best-effort re-populates the cache.
//
// CRITICAL for the D-09 (b) zero-SELECT assertion: when the cache hit succeeds
// this function returns BEFORE any ListActiveServers DB read — the cached blob
// short-circuits the heavy SELECT in both the admin and non-admin branches of
// ListServersCached.
func loadActiveServersCached(c *fiber.Ctx, db *gorm.DB, redisClient *redis.Client, logger *zap.Logger) ([]model.VPNServer, error) {
	if cached, _ := cache.GetServersCache(c.Context(), redisClient); cached != "" {
		var servers []model.VPNServer
		if uerr := json.Unmarshal([]byte(cached), &servers); uerr == nil {
			return servers, nil
		}
		// A corrupt/unparseable cache entry must not break /servers — fall
		// through to the DB (fail-open) and let the next Set overwrite it.
		logger.Warn("servers cache entry unparseable — falling through to DB")
	}

	// Miss / outage / corrupt — load the heavy blob from the DB.
	servers, err := repository.ListActiveServers(c.Context(), db)
	if err != nil {
		return nil, err
	}

	// Best-effort cache write (errors are non-fatal — next request re-populates).
	if blob, mErr := json.Marshal(servers); mErr == nil {
		_ = cache.SetServersCache(c.Context(), redisClient, string(blob))
	} else {
		logger.Warn("failed to marshal servers for cache", zap.Error(mErr))
	}

	return servers, nil
}

// GetServerConfig handles GET /servers/:id/config.
// Returns the connection configuration for a specific server.
func GetServerConfig(logger *zap.Logger, db *gorm.DB, cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		userID := c.Locals("user_id").(string)
		role, _ := c.Locals("role").(string)

		// PAY-11 / D-22: enforce server-access at the handler layer before
		// fetching the server. Admins bypass. Returns 404 (NOT 403) on denial
		// so a lower-tier user can't enumerate paid-tier server UUIDs.
		if role != "admin" {
			planID, perr := resolveUserPlanID(c, db)
			if perr != nil {
				logger.Error("failed to resolve plan_id for GetServerConfig",
					zap.String("user_id", userID), zap.Error(perr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			allowed, aerr := repository.IsServerAllowedForPlan(c.Context(), db, planID, serverID)
			if aerr != nil {
				logger.Error("failed to check plan-server pairing",
					zap.String("plan_id", planID), zap.String("server_id", serverID), zap.Error(aerr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			if !allowed {
				logger.Info("server not allowed for plan — returning 404 (defence in depth)",
					zap.String("user_id", userID), zap.String("plan_id", planID), zap.String("server_id", serverID))
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "server not found",
				})
			}
		}

		server, err := repository.FindServerByID(c.Context(), db, serverID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "server not found",
				})
			}
			logger.Error("failed to find server", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if !server.IsActive {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "server unavailable",
			})
		}

		logger.Debug("serving server config",
			zap.String("server_id", serverID),
			zap.String("user_id", userID),
		)

		// Device limit is enforced at connection registration (POST /connections),
		// not here — config fetch must succeed so the client can attempt to connect.
		config := ServerConfig{
			ServerAddress: server.IPAddress,
			ServerPort:    443,
			Protocol:      server.Protocol,
			// Use the shared tunnel UUID configured via TUNNEL_VLESS_UUID env var.
			// Per-user UUIDs will be implemented when tunnel supports dynamic client management.
			UserID: cfg.TunnelVLESSUUID,
		}

		// Include REALITY config when keys are provisioned.
		// Servers may be AWG-only or WS-only — REALITY is not required.
		if server.RealityPublicKey != "" && server.RealityShortID != "" {
			// Pick a random server_name from the pool so each client session
			// uses a different SNI — makes DPI fingerprinting harder.
			// Note: math/rand is auto-seeded in Go >= 1.20 (we use 1.22).
			serverName := realityServerNames[rand.Intn(len(realityServerNames))]
			config.Reality = &RealityClientConfig{
				PublicKey:   server.RealityPublicKey,
				ShortID:     server.RealityShortID,
				ServerName:  serverName,
				Fingerprint: "chrome",
			}
		}

		// Include WebSocket CDN config when the server has it configured.
		// Clients use this as a fallback when direct REALITY connections are blocked.
		if server.WSEnabled && server.WSHost != "" {
			wsPath := server.WSPath
			if wsPath == "" {
				wsPath = "/ws"
			}
			config.WebSocket = &WebSocketClientConfig{
				Host: server.WSHost,
				Path: wsPath,
			}
		}

		// Include AmneziaWG config when the server has it provisioned.
		// The client can choose to connect via "amneziawg" instead of VLESS when
		// both protocols are available — AmneziaWG offers stronger anti-DPI properties.
		if server.AWGPublicKey != nil && *server.AWGPublicKey != "" &&
			server.AWGEndpoint != nil && *server.AWGEndpoint != "" {

			awgCfg := &AWGClientConfig{
				PublicKey:  *server.AWGPublicKey,
				Endpoint:   *server.AWGEndpoint,
				AllowedIPs: "0.0.0.0/0, ::/0",
			}

			// Copy obfuscation params when present; otherwise they default to zero
			// which means standard WireGuard behaviour (no obfuscation).
			if server.AWGParams != nil {
				awgCfg.Jc = server.AWGParams.Jc
				awgCfg.Jmin = server.AWGParams.Jmin
				awgCfg.Jmax = server.AWGParams.Jmax
				awgCfg.S1 = server.AWGParams.S1
				awgCfg.S2 = server.AWGParams.S2
				awgCfg.H1 = server.AWGParams.H1
				awgCfg.H2 = server.AWGParams.H2
				awgCfg.H3 = server.AWGParams.H3
				awgCfg.H4 = server.AWGParams.H4
			}

			config.AWG = awgCfg

			logger.Debug("including awg config in server response",
				zap.String("server_id", serverID),
				zap.String("awg_endpoint", *server.AWGEndpoint),
			)
		}

		// Verify the server has at least one protocol configured.
		if config.Reality == nil && config.WebSocket == nil && config.AWG == nil {
			logger.Error("server has no protocols configured",
				zap.String("server_id", serverID),
			)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "server has no protocols configured",
			})
		}

		// Compute geo-aware protocol priority for the client.
		// Checks CF-IPCountry header (set by Cloudflare) first, then falls back
		// to a simple country code heuristic.
		region := detectClientRegion(c)
		config.ProtocolPriority = protocolPriorityForRegion(region, config)

		logger.Debug("serving config with protocol priority",
			zap.String("server_id", serverID),
			zap.String("region", region),
			zap.Strings("priority", config.ProtocolPriority),
		)

		return c.JSON(fiber.Map{
			"data": config,
		})
	}
}

// detectClientRegion returns an ISO 3166-1 alpha-2 country code for the client.
func detectClientRegion(c *fiber.Ctx) string {
	// Cloudflare sets this header automatically — most reliable.
	if country := c.Get("CF-IPCountry"); country != "" {
		return country
	}
	// Fallback: could integrate MaxMind GeoLite2 here for non-Cloudflare setups.
	return ""
}

// protocolPriorityForRegion returns the recommended protocol order based on
// the client's country and the server's available protocols.
func protocolPriorityForRegion(region string, cfg ServerConfig) []string {
	var order []string

	switch region {
	case "RU":
		// Russia: CDN WebSocket is hardest to block (would require blocking Cloudflare).
		// AmneziaWG second (obfuscated UDP). REALITY last (TSPU increasingly detects it).
		order = []string{"vless-ws", "amneziawg", "vless-reality"}
	case "IR":
		// Iran: AmneziaWG works well, WS is second.
		order = []string{"amneziawg", "vless-ws", "vless-reality"}
	case "CN":
		// China: CDN tunneling works best, REALITY second.
		order = []string{"vless-ws", "vless-reality", "amneziawg"}
	default:
		// Uncensored regions: REALITY is fastest (no CDN overhead).
		order = []string{"vless-reality", "amneziawg", "vless-ws"}
	}

	// Filter to only include protocols the server actually supports.
	available := make([]string, 0, 3)
	for _, p := range order {
		switch p {
		case "vless-reality":
			if cfg.Reality != nil {
				available = append(available, p)
			}
		case "vless-ws":
			if cfg.WebSocket != nil {
				available = append(available, p)
			}
		case "amneziawg":
			if cfg.AWG != nil {
				available = append(available, p)
			}
		}
	}

	return available
}

// resolveUserPlanID returns the plan_id for the authenticated user.
//
// Until plan 03-07 (JWT plan_id claim) lands, c.Locals("plan_id") is unset —
// fall back to a single DB lookup via FindUserByID. The 5-min access TTL
// bound after 03-07 ships eventually makes the DB fallback dead code, but
// the cost is one indexed PK lookup (~0.5ms) and structurally safer than
// failing 500 on missing claim.
//
// IMPORTANT: after plan 03-07 ships and operators have rolled the JWT shape,
// this helper continues to work — claims.PlanID populates c.Locals via the
// amended middleware. No further code change in this file needed at that point.
func resolveUserPlanID(c *fiber.Ctx, db *gorm.DB) (string, error) {
	if planID, ok := c.Locals("plan_id").(string); ok && planID != "" {
		return planID, nil
	}
	userID, _ := c.Locals("user_id").(string)
	if userID == "" {
		return "", fiber.ErrUnauthorized
	}
	// PERF-04 / D-07: prefer the *model.User AuthRequired already loaded on a
	// cache miss (c.Locals("user")) over a fresh DB read. On a user-cache HIT
	// the local is absent, so fall back to the single FindUserByID below.
	if u, ok := c.Locals("user").(*model.User); ok && u != nil {
		return u.PlanID, nil
	}
	user, err := repository.FindUserByID(c.Context(), db, userID)
	if err != nil {
		return "", err
	}
	return user.PlanID, nil
}
