package middleware

import (
	"strings"
	"time"

	"vpnapp/server/api/internal/cache"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	authenticatedRateLimit   = 200
	unauthenticatedRateLimit = 30
	rateLimitWindow          = 1 * time.Minute

	// LinkCode brute-force defence: at 10 attempts/min/IP an attacker
	// needs ~70 days from one IP to reach a 50% probability of guessing
	// any single 6-digit code, and brute force across all active codes
	// is bounded by the per-IP limit times the number of distinct IPs.
	linkAttemptLimit  = 10
	linkAttemptWindow = 1 * time.Minute

	// /debug/error log-spend bucket (HARD-13 / S7-2): 5 reports per IP per
	// minute. The endpoint writes a log line per call, so an unbounded client
	// can burn log-aggregation budget. The 6th call within the window 429s.
	debugErrorLimit  = 5
	debugErrorWindow = 1 * time.Minute
)

// RateLimit returns per-user rate-limit middleware backed by Redis.
//
// When a valid Bearer token is present in the Authorization header, the JWT is
// decoded (without verifying the signature — expiry/tamper checks happen in
// AuthRequired) to extract the user ID for per-user rate limiting at
// authenticatedRateLimit req/min.  If no token is present, or the token cannot
// be decoded, the request is limited per client IP at unauthenticatedRateLimit
// req/min.
//
// When Redis is unavailable IncrRateLimit returns an error and the request is
// allowed through — the middleware degrades gracefully rather than blocking
// all traffic during a Redis outage.
func RateLimit(redisClient *redis.Client, logger *zap.Logger, jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var key string
		var limit int64

		// First check if auth middleware already set user_id in locals.
		userID, _ := c.Locals("user_id").(string)

		// If not set yet (rate limiter is running before auth middleware),
		// try to extract user_id directly from the Authorization header.
		if userID == "" {
			if authHeader := c.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
				userID = extractUserIDFromToken(tokenStr, jwtSecret)
			}
		}

		if userID != "" {
			key = "user:" + userID
			limit = authenticatedRateLimit
		} else {
			key = "ip:" + c.IP()
			limit = unauthenticatedRateLimit
		}

		count, err := cache.IncrRateLimit(c.Context(), redisClient, key, rateLimitWindow)
		if err != nil {
			// Log but do not block the request — Redis unavailability must not
			// cause a service outage.
			logger.Warn("rate limit check failed, allowing request",
				zap.String("key", key),
				zap.Error(err),
			)
			return c.Next()
		}

		if count > limit {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "too many requests, please slow down",
			})
		}

		return c.Next()
	}
}

// LinkAttemptLimit returns middleware that bounds brute-force attempts on
// /auth/link to 10 per IP per minute. The dedicated bucket prevents link-code
// guessing from being amortised over the broader 30 req/min/IP budget that
// other public endpoints share.
//
// FAIL CLOSED (HARD-12 / SECURITY-AUDIT S7-1): when Redis is unreachable the
// limiter cannot count attempts, so it returns 503 rather than letting the
// request through. Failing open here would let an attacker brute-force
// link codes for the entire duration of a Redis outage. This is the OPPOSITE
// disposition from the dedicated /debug/error bucket (DebugErrorLimit), which
// fails OPEN because a logging endpoint must not break when Redis is down.
// The asymmetry is intentional: a brute-force surface fails closed, a
// log-spend surface fails open. The global RateLimit (above) keeps its
// fail-open behaviour unchanged (D-20 scope).
func LinkAttemptLimit(redisClient *redis.Client, logger *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := "link:" + c.IP()
		count, err := cache.IncrRateLimit(c.Context(), redisClient, key, linkAttemptWindow)
		if err != nil {
			logger.Error("link rate limit check failed — failing closed",
				zap.String("ip", c.IP()),
				zap.Error(err),
			)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "service temporarily unavailable",
			})
		}
		if count > linkAttemptLimit {
			logger.Warn("link rate limit exceeded",
				zap.String("ip", c.IP()),
				zap.Int64("count", count),
			)
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "too many link attempts, try again later",
			})
		}
		return c.Next()
	}
}

// DebugErrorLimit returns middleware that caps /debug/error to 5 reports per
// IP per minute (HARD-13 / SECURITY-AUDIT S7-2). The endpoint writes a log
// line per call, so an unbounded client can burn log-aggregation budget; the
// dedicated bucket bounds that spend without affecting other endpoints.
//
// FAIL OPEN on Redis outage: this is a diagnostics/logging endpoint, and
// breaking it when Redis is down would suppress the very error reports an
// operator needs during an incident. This is the deliberate OPPOSITE of
// LinkAttemptLimit's fail-closed disposition (research §4.9 asymmetry): a
// brute-force surface fails closed, a log-spend surface fails open.
func DebugErrorLimit(redisClient *redis.Client, logger *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := "ratelimit:debug:" + c.IP()
		count, err := cache.IncrRateLimit(c.Context(), redisClient, key, debugErrorWindow)
		if err != nil {
			// Fail OPEN — a logging endpoint must not break on Redis outage.
			logger.Warn("debug/error rate limit check failed, allowing request",
				zap.String("ip", c.IP()),
				zap.Error(err),
			)
			return c.Next()
		}
		if count > debugErrorLimit {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "too many error reports, please slow down",
			})
		}
		return c.Next()
	}
}

// extractUserIDFromToken parses a JWT and returns the "sub" claim without
// performing expiry or signature validation. It is used only for rate-limit key
// selection — AuthRequired performs full validation for protected routes.
func extractUserIDFromToken(tokenStr, jwtSecret string) string {
	token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}

	// Only use the claim if the signature is actually valid — we don't want
	// an attacker to spoof a high-traffic user_id to bypass IP limiting.
	verifiedToken, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	}, jwt.WithoutClaimsValidation())
	if err != nil || !verifiedToken.Valid {
		return ""
	}

	sub, _ := claims["sub"].(string)
	return sub
}
