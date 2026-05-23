package middleware

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Claims holds the JWT payload.
//
// Phase 3 (D-29): adds PlanID with json:"plan_id,omitempty" so in-flight
// Phase 2 JWTs (issued before this plan landed) still parse — the access
// token has a 5-minute TTL, so the backward-compat window closes naturally.
// When the claim is empty AuthRequired falls back to repository.FindUserByID
// (which it already calls for the HOTFIX-02 existence check).
type Claims struct {
	UserID string `json:"sub"`
	Tier   string `json:"tier"`
	Role   string `json:"role"`
	PlanID string `json:"plan_id,omitempty"`
	jwt.RegisteredClaims
}

// AuthRequired is middleware that validates JWT access tokens.
// Extracts the user ID, subscription tier, and role from the token
// and stores them in the Fiber context locals.
//
// When a non-nil Redis client is provided, every request additionally checks
// whether the token has been blacklisted (e.g. after logout). A blacklisted
// token is rejected with 401 even if its signature and expiry are still valid.
//
// When a non-nil db is provided, every request ALSO verifies that the user
// referenced by the token still exists. Without this check, a client holding
// a still-valid JWT for a user that was deleted server-side (by the Telegram
// recovery flow's PerformRestore, admin user-delete, or any future cascade)
// would get 404/500 from every handler's FindUserByID call — but never a
// 401 — so the axios interceptor's refresh + re-auth logic never fires and
// the client is stuck in a zombie state until the user force-clears app
// storage. The extra lookup is a single indexed PK query (~sub-ms) and is
// paid only on authenticated routes.
func AuthRequired(jwtSecret string, redisClient *redis.Client, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract token from Authorization header.
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing authorization header",
			})
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid authorization format",
			})
		}

		// Parse and validate JWT.
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid or expired token",
			})
		}

		// Check token blacklist when Redis is available.
		if redisClient != nil {
			tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenString)))
			if cache.IsTokenBlacklisted(c.Context(), redisClient, tokenHash) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "token has been revoked",
				})
			}
		}

		// Verify the user the JWT references still exists. Returning 401
		// (rather than letting downstream handlers return 404) ensures
		// the client's refresh flow fires, which in turn will fail
		// against a deleted session row and cause the client to fall
		// back to a fresh /auth/guest — the correct recovery path.
		//
		// Phase 3 D-29: reuse the loaded user as the backward-compat
		// fallback source for the plan_id claim — saves a second DB
		// lookup in the 5-minute transition window after this plan
		// ships and JWTs still don't carry the claim.
		var loadedUser *model.User
		if db != nil {
			u, err := repository.FindUserByID(db, claims.UserID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
						"error": "user no longer exists",
					})
				}
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			loadedUser = u
		}

		// Store user info in context for downstream handlers.
		c.Locals("user_id", claims.UserID)
		c.Locals("tier", claims.Tier)
		c.Locals("role", claims.Role)

		// Phase 3 D-29: plan_id from JWT, fall back to the user row already
		// loaded above when the claim is empty (in-flight Phase 2 JWTs).
		// After the 5-min access-token TTL elapses post-deploy, every JWT
		// carries the claim and the fallback branch is dead.
		planID := claims.PlanID
		if planID == "" && loadedUser != nil {
			planID = loadedUser.PlanID
		}
		c.Locals("plan_id", planID)

		return c.Next()
	}
}
