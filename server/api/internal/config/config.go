package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// BcryptCost is the bcrypt work factor used for all NEW or changed password
// hashes (HARD-11 / SECURITY-AUDIT S4-5). Raised from bcrypt.DefaultCost (10)
// to 12 to make offline cracking of an exfiltrated admin hash ~4x more
// expensive. Existing cost-10 hashes still verify because bcrypt embeds the
// cost in the hash string — no migration is needed; only newly written hashes
// (createadmin bootstrap + admin password-change) use the higher cost.
const BcryptCost = 12

// Config holds API server configuration loaded from environment variables.
type Config struct {
	Port                int
	DatabaseURL         string
	RedisURL            string
	JWTSecret           string
	StripeKey           string
	StripeWebhookSecret string
	StripePricePremium  string
	StripePriceUltimate string
	AppDeepLinkScheme   string
	TunnelVLESSUUID     string
	MinAppVersion       string

	// Background scheduler / sharing tunables. Defaults match the
	// previously hard-coded values; expose them so deployments can adjust
	// without recompiling.
	StaleConnectionAfter time.Duration // marks connections without heartbeat as stale
	StaleDeviceAfter     time.Duration // auto-removes idle device rows
	LinkCodeTTL          time.Duration // share-code lifetime before expiry

	// RunScheduler gates whether THIS process runs the background scheduler
	// (cleanup, expiry downgrades, the 10s heartbeat flush, weekly prune).
	// PERF-06 / D-09b: when the API scales to multiple replicas (Tranche 3),
	// only the primary should run periodic jobs — set RUN_SCHEDULER=false on
	// the others. Defaults to true so the single-replica v2.2.0 deploy keeps
	// running periodic work with no config change. Computed from
	// ShouldRunScheduler(getEnv("RUN_SCHEDULER","true")) in Load().
	RunScheduler bool

	// Telegram recovery bot (ADR-006). All three fields are optional —
	// the bot goroutine only starts when RecoveryBotToken is non-empty,
	// and the admin notification on restore is skipped when
	// TelegramAdminChatID is zero.
	RecoveryBotToken    string // @BotFather token for the recovery bot
	RecoveryBotUsername string // without leading @, used in deep links
	TelegramAdminChatID int64  // where to DM on tg_restore events

	// EnvParseWarnings lists the tunable env vars that were set by the
	// operator but failed to parse (e.g. STALE_CONNECTION_AFTER=3min,
	// should be 3m). Load() populates this on each call; cmd/main.go
	// emits a single logger.Warn line after Load() returns so operators
	// see their tuning intent was discarded instead of silently using
	// the hard-coded default. See REVIEW.md WR-05.
	EnvParseWarnings []string

	// SSO — Apple (D-30, Phase 2 AUTH-03).
	// Three required identifiers (TeamID, BundleID, ServiceID) are
	// validated at startup by RequireEnv(). KeyID / PrivateKeyP8 are
	// optional this phase — reserved for the future Apple
	// authorizationCode exchange (D-18).
	AppleTeamID       string
	AppleBundleID     string
	AppleServiceID    string
	AppleKeyID        string // optional this phase (D-30); reserved for authorizationCode exchange
	ApplePrivateKeyP8 string // optional this phase (D-30); reserved for authorizationCode exchange
	// SSO — Google (D-30, Phase 2 AUTH-03). Three distinct OAuth
	// client IDs (one per surface — iOS, Android, web) all required
	// at startup; verifier loops them and accepts the first match.
	GoogleClientIDIOS     string
	GoogleClientIDAndroid string
	GoogleClientIDWeb     string

	// Phase 3 lava.top payment provider (D-30, RESEARCH §13).
	// LavaEnv selects which API key the constructed *lava.Client uses.
	// Strict-required at startup via RequireEnv() — no defaults except LavaEnv ("production").
	LavaEnv                   string // "sandbox" | "production" (default "production" when env unset)
	LavaAPIKey                string // required when LavaEnv == "production"
	LavaAPIKeySandbox         string // required when LavaEnv == "sandbox"
	LavaWebhookSecret         string // required — X-Api-Key on inbound webhook
	LavaWebhookSecretPrevious string // optional — set only during rotation (D-17)
	LavaWebhookAllowedCIDRs   string // required CSV of CIDRs lava webhook sources (D-16 — strict-required per planner's pick)
	LavaSuccessURL            string // required — https://risevpn.com/pay/success
	LavaFailURL               string // required — https://risevpn.com/pay/fail
	// LavaActiveAPIKey is the resolved key based on LavaEnv (populated by Load()).
	// Consumers (lava.Client constructor in cmd/main.go) read this; they never
	// inspect LavaAPIKey vs LavaAPIKeySandbox directly.
	LavaActiveAPIKey string

	// InternalHeartbeatSecret is the shared secret the tunnel server presents in
	// the X-Internal-Secret header on POST /api/v1/internal/servers/:id/heartbeat
	// (ADMIN-07, T-07-08). It is a real auth secret, so RequireEnv() fails fast at
	// boot when it is unset — the /internal group must never be reachable without it.
	InternalHeartbeatSecret string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	port, err := strconv.Atoi(getEnv("PORT", "3000"))
	if err != nil {
		return nil, fmt.Errorf("invalid PORT: %w", err)
	}

	// Collect tunable-env parse failures here. The logger is not yet
	// alive at the time these helpers run, so we stash them on the
	// Config and let cmd/main.go emit a single warn line post-Load.
	// See REVIEW.md WR-05 — the prior implementation silently swallowed
	// invalid values like STALE_CONNECTION_AFTER=3min (should be 3m).
	var parseWarnings []string

	cfg := &Config{
		Port:                 port,
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://localhost:5432/vpnapp?sslmode=disable"),
		RedisURL:             getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:            getEnv("JWT_SECRET", ""),
		StripeKey:            getEnv("STRIPE_KEY", ""),
		StripeWebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripePricePremium:   getEnv("STRIPE_PRICE_PREMIUM", "price_PLACEHOLDER_PREMIUM"),
		StripePriceUltimate:  getEnv("STRIPE_PRICE_ULTIMATE", "price_PLACEHOLDER_ULTIMATE"),
		AppDeepLinkScheme:    getEnv("APP_DEEP_LINK", "vpnapp"),
		TunnelVLESSUUID:      getEnv("TUNNEL_VLESS_UUID", ""),
		MinAppVersion:        getEnv("MIN_APP_VERSION", "2.0.0"),
		StaleConnectionAfter: getEnvDuration("STALE_CONNECTION_AFTER", 3*time.Minute, &parseWarnings),
		StaleDeviceAfter:     getEnvDuration("STALE_DEVICE_AFTER", 30*24*time.Hour, &parseWarnings),
		LinkCodeTTL:          getEnvDuration("LINK_CODE_TTL", 5*time.Minute, &parseWarnings),
		RunScheduler:         ShouldRunScheduler(getEnv("RUN_SCHEDULER", "true")),

		RecoveryBotToken:    getEnv("TELEGRAM_RECOVERY_BOT_TOKEN", ""),
		RecoveryBotUsername: getEnv("TELEGRAM_RECOVERY_BOT_USERNAME", "risevp_bot"),
		TelegramAdminChatID: getEnvInt64("TELEGRAM_ADMIN_CHAT_ID", 0, &parseWarnings),
	}
	cfg.EnvParseWarnings = parseWarnings

	// SSO env (D-30, Phase 2 AUTH-03). RequireEnv() (below) enforces
	// the six required keys at boot; the two optional Apple .p8 keys
	// surface as a single WARN line via OptionalEnvWarnings.
	cfg.AppleTeamID = getEnv("APPLE_TEAM_ID", "")
	cfg.AppleBundleID = getEnv("APPLE_BUNDLE_ID", "")
	cfg.AppleServiceID = getEnv("APPLE_SERVICE_ID", "")
	cfg.AppleKeyID = getEnv("APPLE_KEY_ID", "")
	cfg.ApplePrivateKeyP8 = getEnv("APPLE_PRIVATE_KEY_P8", "")
	cfg.GoogleClientIDIOS = getEnv("GOOGLE_CLIENT_ID_IOS", "")
	cfg.GoogleClientIDAndroid = getEnv("GOOGLE_CLIENT_ID_ANDROID", "")
	cfg.GoogleClientIDWeb = getEnv("GOOGLE_CLIENT_ID_WEB", "")

	// Phase 3 lava.top (D-30).
	cfg.LavaEnv = getEnv("LAVA_ENV", "production")
	cfg.LavaAPIKey = getEnv("LAVA_API_KEY", "")
	cfg.LavaAPIKeySandbox = getEnv("LAVA_API_KEY_SANDBOX", "")
	cfg.LavaWebhookSecret = getEnv("LAVA_WEBHOOK_SECRET", "")
	cfg.LavaWebhookSecretPrevious = getEnv("LAVA_WEBHOOK_SECRET_PREVIOUS", "")
	cfg.LavaWebhookAllowedCIDRs = getEnv("LAVA_WEBHOOK_ALLOWED_CIDRS", "")
	cfg.LavaSuccessURL = getEnv("LAVA_SUCCESS_URL", "")
	cfg.LavaFailURL = getEnv("LAVA_FAIL_URL", "")

	// Phase 7 (ADMIN-07): tunnel-heartbeat shared secret. Required at boot.
	cfg.InternalHeartbeatSecret = getEnv("INTERNAL_HEARTBEAT_SECRET", "")
	// Resolve active key once at startup; RequireEnv enforces non-empty.
	switch cfg.LavaEnv {
	case "sandbox":
		cfg.LavaActiveAPIKey = cfg.LavaAPIKeySandbox
	default: // "production" or anything else (RequireEnv catches invalid values)
		cfg.LavaActiveAPIKey = cfg.LavaAPIKey
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	if cfg.TunnelVLESSUUID == "" {
		return nil, fmt.Errorf("TUNNEL_VLESS_UUID is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// ShouldRunScheduler decides, from the raw RUN_SCHEDULER env value, whether this
// process should run the background scheduler (PERF-06 / D-09b). It is a PURE,
// side-effect-free helper so main.go's branch is unit-testable without spinning
// a real second replica (the load-bearing PERF-06 (d) assertion).
//
// Default ON: ANY value EXCEPT the explicit falsey set {"false","0","no"}
// (case-insensitive, trimmed) enables the scheduler — including the empty string
// (env var unset), so the single-replica v2.2.0 deploy keeps running periodic
// work with no config change. To DISABLE on a non-primary replica, set
// RUN_SCHEDULER to one of false / 0 / no.
func ShouldRunScheduler(envValue string) bool {
	switch strings.ToLower(strings.TrimSpace(envValue)) {
	case "false", "0", "no":
		return false
	default:
		return true
	}
}

// getEnvDuration parses a Go duration string from the environment, falling
// back to the default if the var is unset or unparseable. When a value is
// set but fails to parse, the key (with a short reason) is appended to
// warnings so the caller can surface the discarded operator intent.
func getEnvDuration(key string, fallback time.Duration, warnings *[]string) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		if warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf("%s=%q (invalid duration: %v) — using default %s", key, val, err, fallback))
		}
		return fallback
	}
	return d
}

// getEnvInt64 parses a signed 64-bit integer from the environment.
// Returns fallback when the var is missing or unparseable. Mirrors
// getEnvDuration's parse-warning channel so the caller can surface
// discarded operator intent.
func getEnvInt64(key string, fallback int64, warnings *[]string) int64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		if warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf("%s=%q (invalid int64: %v) — using default %d", key, val, err, fallback))
		}
		return fallback
	}
	return n
}

// RequireEnv reports every required environment variable that is unset or empty.
// Returns an empty slice when all required vars are set.
//
// Single-pass aggregate validator (per HOTFIX-08 D-04): scans every var in one
// call so the operator sees ALL missing keys in one error, not "fix one, restart,
// fix the next". Called from cmd/main.go BEFORE config.Load(); a non-empty return
// becomes a logger.Fatal which calls os.Exit(1).
//
// Phase 3 (D-30): adds LAVA_* keys. Per planner's pick of strict-required
// (RESEARCH §13.3), LAVA_WEBHOOK_ALLOWED_CIDRS has no default — the operator
// MUST supply the CIDR list explicitly. LAVA_ENV defaults to "production"
// when unset; the matching LAVA_API_KEY / LAVA_API_KEY_SANDBOX is then required.
func RequireEnv() []string {
	required := []string{
		"JWT_SECRET",
		"DATABASE_URL",
		"REDIS_URL",
		"TUNNEL_VLESS_UUID",
		// SSO required (Phase 2 D-30):
		"APPLE_TEAM_ID",
		"APPLE_BUNDLE_ID",
		"APPLE_SERVICE_ID",
		"GOOGLE_CLIENT_ID_IOS",
		"GOOGLE_CLIENT_ID_ANDROID",
		"GOOGLE_CLIENT_ID_WEB",
		// Phase 3 lava.top required (D-30):
		"LAVA_WEBHOOK_SECRET",
		"LAVA_WEBHOOK_ALLOWED_CIDRS",
		"LAVA_SUCCESS_URL",
		"LAVA_FAIL_URL",
		// Phase 7 (ADMIN-07 / T-07-08): tunnel-heartbeat shared secret.
		"INTERNAL_HEARTBEAT_SECRET",
	}
	var missing []string
	for _, key := range required {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	// LAVA_ENV + matching API key compound check. LAVA_ENV defaults to "production"
	// when unset; values other than "sandbox"/"production" are rejected here.
	lavaEnv := os.Getenv("LAVA_ENV")
	if lavaEnv == "" {
		lavaEnv = "production"
	}
	switch lavaEnv {
	case "production":
		if os.Getenv("LAVA_API_KEY") == "" {
			missing = append(missing, "LAVA_API_KEY (required when LAVA_ENV=production)")
		}
	case "sandbox":
		if os.Getenv("LAVA_API_KEY_SANDBOX") == "" {
			missing = append(missing, "LAVA_API_KEY_SANDBOX (required when LAVA_ENV=sandbox)")
		}
	default:
		missing = append(missing, fmt.Sprintf("LAVA_ENV=%q (must be 'sandbox' or 'production')", lavaEnv))
	}
	return missing
}

// OptionalEnvWarnings reports payment-provider env vars that are unset, empty,
// or set to a known placeholder string. These do NOT block startup but should
// emit a single warn-log line so misconfigured deploys are visible.
//
// STRIPE_* are warned because Stripe leaves in Phase 8; once gone, this list
// shrinks. LAVA_* will move to RequireEnv in Phase 3.
func OptionalEnvWarnings() []string {
	optional := map[string]string{
		"STRIPE_KEY":            "",
		"STRIPE_WEBHOOK_SECRET": "",
		"STRIPE_PRICE_PREMIUM":  "price_PLACEHOLDER_PREMIUM",
		"STRIPE_PRICE_ULTIMATE": "price_PLACEHOLDER_ULTIMATE",
		// SSO optional (D-30, Phase 2 AUTH-03) — reserved for future Apple authorizationCode exchange:
		"APPLE_KEY_ID":         "",
		"APPLE_PRIVATE_KEY_P8": "",
	}
	var warned []string
	for key, placeholder := range optional {
		val := os.Getenv(key)
		if val == "" || (placeholder != "" && val == placeholder) {
			warned = append(warned, key)
		}
	}
	return warned
}
