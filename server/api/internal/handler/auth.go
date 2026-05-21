package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// AdminLogin handles POST /auth/admin-login.
// Validates email+password against DB and returns JWT tokens ONLY if the user
// has role='admin'. Non-admin users receive the same "invalid credentials"
// error as a wrong password (no role-enumeration leak).
func AdminLogin(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req loginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		if req.Email == "" || req.Password == "" || len(req.Password) > 72 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "email and password required (max 72 chars)",
			})
		}

		if !strings.Contains(req.Email, "@") || len(req.Email) < 5 || len(req.Email) > 255 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid email format",
			})
		}

		// Find user by email hash
		emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.Email)))
		user, err := repository.FindUserByEmailHash(db, emailHash)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "invalid credentials",
				})
			}
			logger.Error("admin-login: failed to find user", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Verify password
		if user.PasswordHash == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid credentials",
			})
		}
		if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid credentials",
			})
		}

		// Enforce admin role — non-admins get the same error as wrong password
		if user.Role != "admin" {
			logger.Warn("admin-login: non-admin user attempted admin login", zap.String("user_id", user.ID))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid credentials",
			})
		}

		tokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, cfg.JWTSecret)
		if err != nil {
			logger.Error("admin-login: failed to generate tokens", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
			logger.Error("admin-login: failed to store session", zap.Error(err))
		}

		logger.Info("admin logged in", zap.String("user_id", user.ID))

		return c.JSON(fiber.Map{
			"data": tokens,
		})
	}
}

// AdminChangePassword handles POST /admin/change-password.
// Requires the caller to be an authenticated admin (enforced by the
// admin route group). The request body must carry the current_password
// so a stolen access token alone can't rotate credentials — the
// attacker would need the current password too.
//
// On success the existing refresh sessions are NOT invalidated, so the
// admin's other tabs keep working until their access token expires in
// the normal 5-minute cycle. If you want aggressive invalidation, call
// this endpoint from a "sign out everywhere" button — that's a future
// enhancement.
func AdminChangePassword(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		adminID, _ := c.Locals("user_id").(string)
		if adminID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}

		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}
		if req.CurrentPassword == "" || req.NewPassword == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "current_password and new_password required",
			})
		}
		// bcrypt caps plaintext at 72 bytes — reject anything longer
		// so users get a clear error instead of a silent truncation.
		if len(req.NewPassword) < 8 || len(req.NewPassword) > 72 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "new_password must be 8..72 characters",
			})
		}

		user, err := repository.FindUserByIDAdmin(db, adminID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// Admin's JWT references a user that no longer exists —
				// stale session. Don't log as an error.
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "unauthorized",
				})
			}
			logger.Error("change-password: failed to load admin user",
				zap.String("admin_id", adminID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		if user.PasswordHash == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "account has no password",
			})
		}
		if err := bcrypt.CompareHashAndPassword(
			[]byte(*user.PasswordHash),
			[]byte(req.CurrentPassword),
		); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "current password is incorrect",
			})
		}

		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			logger.Error("change-password: bcrypt failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		hashStr := string(newHash)
		if err := repository.UpdateUser(db, adminID, map[string]interface{}{
			"password_hash": &hashStr,
		}); err != nil {
			logger.Error("change-password: UpdateUser failed",
				zap.String("admin_id", adminID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Info("admin password changed", zap.String("admin_id", adminID))
		return c.JSON(fiber.Map{"data": fiber.Map{"success": true}})
	}
}

// RefreshToken handles POST /auth/refresh.
// Validates refresh token, rotates tokens, returns new pair.
func RefreshToken(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.BodyParser(&req); err != nil || req.RefreshToken == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "refresh_token required",
			})
		}

		// Find session by refresh token hash
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.RefreshToken)))
		session, err := repository.FindSessionByTokenHash(db, tokenHash)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "invalid or expired refresh token",
				})
			}
			logger.Error("failed to find session", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Rotate session inside a single transaction so a failed insert never
		// leaves the user with no session row. Per SECURITY-AUDIT S1-1.
		//
		// On any error inside the transaction (delete failure, lookup failure,
		// token generation failure, insert failure), GORM rolls back the
		// delete — the old session row stays in place and the user can refresh
		// again later. The client sees a 500 and retries the refresh.
		//
		// All DB operations inside the closure use tx, never the outer db.
		// Tokens are assigned to the outer-scoped variable only after the
		// closure returns nil, so the HTTP response is only emitted when the
		// new session row has committed.
		var tokens *authResponse
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repository.DeleteSession(tx, session.ID); err != nil {
				return fmt.Errorf("deleting old session: %w", err)
			}

			// Re-read the user inside the transaction so tier/role/name are
			// consistent with the token we're about to mint.
			user, err := repository.FindUserByID(tx, session.UserID)
			if err != nil {
				return fmt.Errorf("loading user: %w", err)
			}

			newTokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, cfg.JWTSecret)
			if err != nil {
				return fmt.Errorf("generating tokens: %w", err)
			}

			// storeRefreshSession accepts any *gorm.DB; tx is one. Reusing the
			// existing helper keeps the SHA-256 hashing and expiry logic in one
			// place. The new session row is inserted in the same tx as the
			// delete, providing the atomicity guarantee.
			if err := storeRefreshSession(tx, user.ID, newTokens.RefreshToken); err != nil {
				return fmt.Errorf("storing new session: %w", err)
			}

			tokens = newTokens
			return nil
		})
		if err != nil {
			// Distinguish "user gone" (401 — client should re-authenticate)
			// from other DB errors (500 — client should retry).
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("refresh rotation failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		return c.JSON(fiber.Map{
			"data": tokens,
		})
	}
}

// guestLoginRequest is the body sent by the mobile app on /auth/guest.
// All fields are optional; older clients that omit device_id will still
// get a fresh anonymous account on every call (legacy behaviour).
//
// device_secret pairs with device_id to defeat impersonation by knowledge
// of device_id alone. The client generates a random 32-byte secret on
// first launch and stores it in app-private storage; only the SHA-256 hash
// is persisted on the server. See migration 012 for the threat model.
type guestLoginRequest struct {
	DeviceID     string `json:"device_id"`
	DeviceSecret string `json:"device_secret"`
	Platform     string `json:"platform"`
	Model        string `json:"model"`
}

// hashDeviceSecret returns the lowercase hex SHA-256 of the given secret.
// Returns "" for an empty input so the caller can distinguish "no secret
// provided" from "valid empty hash".
func hashDeviceSecret(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("%x", sum)
}

// GuestLogin handles POST /auth/guest.
//
// If the request includes a device_id and that device has authenticated
// before, the existing user_id is returned (and the device row is touched).
// This makes guest sessions stable across app reinstalls and across the
// "share code" link flow — the same physical device always maps to the
// same account.
//
// Authentication of the device is two-factor:
//   - device_id: the OS-issued identifier (ANDROID_ID / IDFV)
//   - device_secret: a 32-byte random value the client generates on first
//     launch and stores in app-private storage
//
// On match the existing user is returned. On secret mismatch the call
// silently falls through to the "mint a fresh user" path — no error, no
// enumeration. Legacy device rows that have no secret on file accept the
// first secret presented and store its hash (grace-period rollout).
//
// Without a device_id, the legacy behaviour is preserved: a fresh
// anonymous user_id is minted on every call.
func GuestLogin(logger *zap.Logger, db *gorm.DB, cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req guestLoginRequest
		_ = c.BodyParser(&req) // body is optional

		secretHash := hashDeviceSecret(req.DeviceSecret)

		// Fast path: known device — reuse the bound user, no DB churn beyond
		// a touch (and secret-hash population for legacy rows).
		if req.DeviceID != "" {
			if device, err := repository.FindDeviceByDeviceID(db, req.DeviceID); err == nil {
				// Verify the secret. Two acceptable cases:
				//   1. row has hash AND request hash matches → ok
				//   2. row has empty hash AND request provided one → ok, store it
				// Anything else (mismatch, or empty hash + empty request) → fall
				// through to fresh-user path. Empty + empty is safe because
				// such legacy rows will be rejected after the grace period;
				// it just means the client doesn't yet send a secret.
				switch {
				case device.DeviceSecretHash != "" &&
					subtle.ConstantTimeCompare([]byte(device.DeviceSecretHash), []byte(secretHash)) == 1:
					// authenticated — constant-time compare prevents timing
					// side channels from leaking the prefix of a hash.
				case device.DeviceSecretHash == "" && secretHash != "":
					// legacy row, populate the hash on first secret-bearing call
					_ = repository.SetDeviceSecretHash(db, device.ID, secretHash)
				case device.DeviceSecretHash == "" && secretHash == "":
					// legacy row, legacy client — accept (grace period)
				default:
					// secret mismatch — silently fall through to mint a fresh user
					logger.Warn("guest login: device secret mismatch",
						zap.String("device_id", req.DeviceID),
					)
					goto freshUser
				}

				_ = repository.TouchDevice(db, device.ID)
				user, err := repository.FindUserByID(db, device.UserID)
				if err != nil {
					logger.Error("guest login: device user missing",
						zap.String("device_id", req.DeviceID),
						zap.String("user_id", device.UserID),
						zap.Error(err),
					)
					// Fall through to fresh-user path so the user is not locked out.
				} else {
					tokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, cfg.JWTSecret)
					if err != nil {
						logger.Error("guest login: failed to generate tokens for known device", zap.Error(err))
						return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
							"error": "internal server error",
						})
					}
					_ = storeRefreshSession(db, user.ID, tokens.RefreshToken)
					logger.Info("guest login: returning known device",
						zap.String("user_id", user.ID),
						zap.String("device_id", req.DeviceID),
					)
					return c.Status(fiber.StatusOK).JSON(fiber.Map{"data": tokens})
				}
			} else if !errors.Is(err, repository.ErrNotFound) {
				logger.Error("guest login: device lookup failed", zap.Error(err))
				// Fall through to fresh-user path.
			}
		}
	freshUser:

		// Slow path: brand-new device (or device_id not provided). Mint a
		// fresh anonymous user, free subscription, and (when device_id is
		// present) bind it to the new user.
		suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
		guestName := "guest_" + suffix

		user := model.User{
			FullName: guestName,
			// EmailHash and PasswordHash left nil — guest account
		}
		if err := repository.CreateUser(db, &user); err != nil {
			logger.Error("failed to create guest user", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		sub := model.Subscription{
			UserID:   user.ID,
			Plan:     "free",
			IsActive: true,
		}
		if err := repository.CreateSubscription(db, &sub); err != nil {
			logger.Error("failed to create guest subscription — rolling back user",
				zap.String("user_id", user.ID),
				zap.Error(err),
			)
			if deleteErr := repository.DeleteUser(db, user.ID); deleteErr != nil {
				logger.Error("failed to roll back guest user after subscription failure",
					zap.String("user_id", user.ID),
					zap.Error(deleteErr),
				)
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Bind device to the freshly-created user. We always create a NEW
		// row here even if the secret mismatched on a row with the same
		// device_id — that old row stays in the database, owned by its old
		// user, and the legitimate owner can still authenticate against it.
		// (This is the case where a leaked device_id was used by an attacker:
		// they get a fresh anonymous account, the real owner is unaffected.)
		//
		// However, the unique index on devices(device_id) means we can't
		// insert a second row with the same device_id. To handle this we
		// only insert when no row exists; otherwise we leave the existing
		// row alone and the attacker is bound to a brand-new user with no
		// device record at all.
		if req.DeviceID != "" {
			if existing, err := repository.FindDeviceByDeviceID(db, req.DeviceID); err != nil && errors.Is(err, repository.ErrNotFound) {
				device := model.Device{
					UserID:           user.ID,
					DeviceID:         req.DeviceID,
					DeviceSecretHash: secretHash,
					Platform:         req.Platform,
					Model:            req.Model,
				}
				if err := repository.CreateDevice(db, &device); err != nil {
					// Non-fatal: if a race created the device row in parallel, the
					// next call to /auth/guest will hit the fast path. Just log.
					logger.Warn("guest login: device bind failed",
						zap.String("user_id", user.ID),
						zap.String("device_id", req.DeviceID),
						zap.Error(err),
					)
				}
			} else if err == nil {
				// Existing row stayed put because the secret didn't match.
				// Log for security ops.
				logger.Warn("guest login: minted fresh user for device with mismatched secret",
					zap.String("device_id", req.DeviceID),
					zap.String("existing_user_id", existing.UserID),
					zap.String("new_user_id", user.ID),
				)
			}
		}

		tokens, err := generateTokens(user.ID, "free", "user", user.FullName, cfg.JWTSecret)
		if err != nil {
			logger.Error("failed to generate guest tokens", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
			logger.Error("failed to store guest session", zap.Error(err))
		}

		logger.Info("guest user created",
			zap.String("user_id", user.ID),
			zap.String("name", guestName),
			zap.String("device_id", req.DeviceID),
		)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": tokens,
		})
	}
}

// storeRefreshSession hashes the refresh token and stores it in the sessions table.
func storeRefreshSession(db *gorm.DB, userID, refreshToken string) error {
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(refreshToken)))
	session := model.Session{
		UserID:           userID,
		RefreshTokenHash: tokenHash,
		ExpiresAt:        time.Now().Add(30 * 24 * time.Hour),
	}
	return repository.CreateSession(db, &session)
}

// generateTokens creates a JWT access token (5 min) and refresh token (30 days).
// The role claim is embedded in the access token for admin middleware checks.
// The name claim carries the user's display name so the app can show it without
// a separate /account call immediately after login/register.
//
// Access token TTL is intentionally short so admin role changes take effect
// quickly. The connection handler reads tier directly from the DB anyway.
func generateTokens(userID, tier, role, name, secret string) (*authResponse, error) {
	now := time.Now()
	accessExpiry := now.Add(5 * time.Minute)

	accessClaims := jwt.MapClaims{
		"sub":  userID,
		"tier": tier,
		"role": role,
		"name": name,
		"iat":  now.Unix(),
		"exp":  accessExpiry.Unix(),
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessString, err := accessToken.SignedString([]byte(secret))
	if err != nil {
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	refreshClaims := jwt.MapClaims{
		"sub":  userID,
		"type": "refresh",
		"iat":  now.Unix(),
		"exp":  now.Add(30 * 24 * time.Hour).Unix(),
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshString, err := refreshToken.SignedString([]byte(secret))
	if err != nil {
		return nil, fmt.Errorf("signing refresh token: %w", err)
	}

	return &authResponse{
		AccessToken:  accessString,
		RefreshToken: refreshString,
		ExpiresIn:    int(time.Until(accessExpiry).Seconds()),
	}, nil
}
