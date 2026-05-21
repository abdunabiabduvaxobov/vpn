package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

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

	// Telegram recovery bot (ADR-006). All three fields are optional —
	// the bot goroutine only starts when RecoveryBotToken is non-empty,
	// and the admin notification on restore is skipped when
	// TelegramAdminChatID is zero.
	RecoveryBotToken    string // @BotFather token for the recovery bot
	RecoveryBotUsername string // without leading @, used in deep links
	TelegramAdminChatID int64  // where to DM on tg_restore events
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	port, err := strconv.Atoi(getEnv("PORT", "3000"))
	if err != nil {
		return nil, fmt.Errorf("invalid PORT: %w", err)
	}

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
		StaleConnectionAfter: getEnvDuration("STALE_CONNECTION_AFTER", 3*time.Minute),
		StaleDeviceAfter:     getEnvDuration("STALE_DEVICE_AFTER", 30*24*time.Hour),
		LinkCodeTTL:          getEnvDuration("LINK_CODE_TTL", 5*time.Minute),

		RecoveryBotToken:    getEnv("TELEGRAM_RECOVERY_BOT_TOKEN", ""),
		RecoveryBotUsername: getEnv("TELEGRAM_RECOVERY_BOT_USERNAME", "risevp_bot"),
		TelegramAdminChatID: getEnvInt64("TELEGRAM_ADMIN_CHAT_ID", 0),
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

// getEnvDuration parses a Go duration string from the environment, falling
// back to the default if the var is unset or unparseable.
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return fallback
	}
	return d
}

// getEnvInt64 parses a signed 64-bit integer from the environment.
// Returns fallback when the var is missing or unparseable.
func getEnvInt64(key string, fallback int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
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
// Required set is the v2.1.0 runtime-dependency core only (D-03). Stripe vars
// are intentionally OPTIONAL (see OptionalEnvWarnings) because Stripe leaves in
// Phase 8. LAVA_* keys will be added in Phase 3 when lava.top integration lands.
func RequireEnv() []string {
	required := []string{
		"JWT_SECRET",
		"DATABASE_URL",
		"REDIS_URL",
		"TUNNEL_VLESS_UUID",
	}
	var missing []string
	for _, key := range required {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
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
