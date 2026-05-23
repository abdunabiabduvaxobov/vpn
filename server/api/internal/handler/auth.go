package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"vpnapp/server/api/internal/auth/apple"
	"vpnapp/server/api/internal/auth/google"
	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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

		tokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, user.PlanID, cfg.JWTSecret)
		if err != nil {
			logger.Error("admin-login: failed to generate tokens", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
			// Don't hand back a token that has no backing session row — the
			// next /auth/refresh will 401 and the user will be silently
			// signed out. Failing the login lets the client retry cleanly.
			logger.Error("admin-login: failed to store session",
				zap.String("user_id", user.ID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
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

			newTokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, user.PlanID, cfg.JWTSecret)
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
					tokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, user.PlanID, cfg.JWTSecret)
					if err != nil {
						logger.Error("guest login: failed to generate tokens for known device", zap.Error(err))
						return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
							"error": "internal server error",
						})
					}
					if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
						// Without a session row the token we'd return is dead on
						// arrival — the next /auth/refresh would 401. Fail the
						// request so the client retries cleanly; the device row
						// is unchanged so the retry hits this same fast path.
						logger.Error("guest login: failed to store known-device session",
							zap.String("user_id", user.ID),
							zap.String("device_id", req.DeviceID),
							zap.Error(err),
						)
						return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
							"error": "internal server error",
						})
					}
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

		// Phase 3 D-29: assign system plan_id to the fresh guest user so their
		// JWT carries plan_id from the very first token. Repository's
		// FindSystemPlanID returns the UUID of the single is_system=true plan
		// (idx_plans_one_system partial unique enforces exactly one row).
		// Failure is non-fatal — the middleware's DB fallback path covers it.
		if systemPlanID, sysErr := repository.FindSystemPlanID(db); sysErr == nil && systemPlanID != "" {
			if uErr := db.Model(&model.User{}).Where("id = ?", user.ID).Update("plan_id", systemPlanID).Error; uErr != nil {
				logger.Warn("guest login: failed to set system plan_id on fresh user (continuing)",
					zap.String("user_id", user.ID),
					zap.Error(uErr),
				)
			} else {
				user.PlanID = systemPlanID
			}
		}

		tokens, err := generateTokens(user.ID, "free", "user", user.FullName, user.PlanID, cfg.JWTSecret)
		if err != nil {
			logger.Error("failed to generate guest tokens", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
			// User + subscription rows were just created, but without a
			// session row the access token we'd return is dead on arrival
			// (the next /auth/refresh would 401). Fail the request — the
			// device row, when present, will be reused on retry so the
			// caller is not duplicating accounts.
			logger.Error("guest login: failed to store fresh-user session",
				zap.String("user_id", user.ID),
				zap.String("device_id", req.DeviceID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
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
//
// Phase 3 (D-29): adds the `plan_id` claim so server-access enforcement at the
// handler layer can skip a DB lookup per request. Backward-compat: empty
// planID is OK — the middleware (middleware/auth.go) falls back to FindUserByID
// for the 5-minute access-token transition window.
func generateTokens(userID, tier, role, name, planID, secret string) (*authResponse, error) {
	now := time.Now()
	accessExpiry := now.Add(5 * time.Minute)

	accessClaims := jwt.MapClaims{
		"sub":     userID,
		"tier":    tier,
		"role":    role,
		"name":    name,
		"plan_id": planID,
		"iat":     now.Unix(),
		"exp":     accessExpiry.Unix(),
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

// --- Phase 2 SSO handlers (AUTH-01, AUTH-02, AUTH-04, AUTH-05, AUTH-06, AUTH-07) ---

// appleVerifier is the minimal interface AppleSignIn needs from the verifier.
// Production wires *apple.Verifier (satisfies via structural typing). Tests
// inject a fake. (RESEARCH.md §Testing Strategy option A.)
type appleVerifier interface {
	Verify(ctx context.Context, identityToken string) (apple.AppleIdentity, error)
}

// googleVerifier is the matching interface for GoogleSignIn.
type googleVerifier interface {
	Verify(ctx context.Context, idToken string) (google.GoogleIdentity, error)
}

type appleSignInRequest struct {
	IdentityToken     string `json:"identityToken"`
	AuthorizationCode string `json:"authorizationCode"` // D-18: accepted, not exchanged this phase
	FullName          string `json:"fullName"`
	Email             string `json:"email"`
	DeviceID          string `json:"deviceId"`
	DeviceSecret      string `json:"deviceSecret"`
	Platform          string `json:"platform"`
	Model             string `json:"model"`
}

type googleSignInRequest struct {
	IDToken      string `json:"idToken"`
	DeviceID     string `json:"deviceId"`
	DeviceSecret string `json:"deviceSecret"`
	Platform     string `json:"platform"`
	Model        string `json:"model"`
}

// parseGuestJWT decodes and verifies an optional Authorization: Bearer guest JWT.
// Returns the guest user_id when valid, "" when the header is absent, or an
// error when the header is present but malformed/invalid-sig (T-2-GuestJWTSpoof)
// OR when the token's role claim is anything other than empty/"user"
// (WR-01 — an admin token presented here would otherwise be silently treated
// as a promotion carrier and attach a new provider sub to the admin row).
func parseGuestJWT(authHeader, secret string) (string, error) {
	if authHeader == "" {
		return "", nil
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		// Header didn't start with "Bearer " — treat as absent.
		return "", nil
	}
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return "", err
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", errors.New("guest jwt: missing sub")
	}
	// WR-01: only guest/user tokens may carry a guest-promotion intent.
	// An admin access token presented here would otherwise attach a new
	// provider sub to the admin row, silently demoting auth_provider.
	if role, _ := claims["role"].(string); role != "" && role != "user" {
		return "", errors.New("guest jwt: non-user role not allowed for promotion")
	}
	return sub, nil
}

// ssoResolveParams carries the inputs to the shared FindOrCreate-with-race-fallback
// path used by both AppleSignIn and GoogleSignIn.
type ssoResolveParams struct {
	provider       string // "apple" or "google"
	sub            string
	email          string
	emailVerified  bool
	isPrivateRelay bool
	fullName       string
	guestUserID    string // "" if no guest-promotion attempt
}

// findUserByProviderID dispatches between FindUserByAppleID and FindUserByGoogleID.
func findUserByProviderID(db *gorm.DB, provider, sub string) (*model.User, error) {
	switch provider {
	case "apple":
		return repository.FindUserByAppleID(db, sub)
	case "google":
		return repository.FindUserByGoogleID(db, sub)
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

// resolveSSOUser encapsulates the FindOrCreate-with-race-fallback pattern
// shared between Apple and Google sign-in flows. Returns the resolved user.
//
// Composition (per RESEARCH.md §Account-Linking Race Condition + §Guest-Promote-in-Place):
//
//	Step A: findByProvider(sub) — does a row already own this sub?
//	        If yes AND guestUserID is set + different → reassign guest's devices
//	        to the existing row and orphan the guest user, inside db.Transaction
//	        (D-06 / B-3 fix). Return the existing row.
//	Step B: try auto-link by verified email (D-03/D-04 — only when email_verified
//	        AND !is_private_relay).
//	Step C: if guestUserID set, PromoteGuestToSSO (D-06 in-place).
//	Step D: otherwise CreateUser with the provider sub set; on ErrDuplicate
//	        re-read via findByProvider (race lost — W-4 fallback keeps every
//	        concurrent caller on the 200 path).
func resolveSSOUser(db *gorm.DB, logger *zap.Logger, p ssoResolveParams) (*model.User, error) {
	// CR-01 defense in depth: empty sub MUST never reach Step A's FindUserByProviderID.
	// A "successful" verifier.Verify() can still produce an empty Sub if the JWT
	// has no `sub` claim (claims["sub"].(string) silently yields ""). The handlers
	// also guard at the entry point — this is the inner backstop.
	if p.sub == "" {
		return nil, errors.New("sso: empty provider sub")
	}

	// Step A: does a row already own this provider sub?
	existing, err := findUserByProviderID(db, p.provider, p.sub)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		// D-06 reassign-and-orphan branch (B-3 fix): if a guest JWT is presented
		// alongside a sub that's already owned, move the guest's devices to the
		// existing row and delete the (now-stale) guest user row. Both writes
		// happen inside a single db.Transaction so neither can leak partial state.
		if p.guestUserID != "" && p.guestUserID != existing.ID {
			txErr := db.Transaction(func(tx *gorm.DB) error {
				if _, rErr := repository.ReassignDevicesByUserID(tx, p.guestUserID, existing.ID); rErr != nil {
					return fmt.Errorf("reassign devices: %w", rErr)
				}
				if dErr := repository.DeleteOrphanGuestUser(tx, p.guestUserID); dErr != nil &&
					!errors.Is(dErr, repository.ErrNotFound) {
					return fmt.Errorf("delete orphan guest: %w", dErr)
				}
				return nil
			})
			if txErr != nil {
				logger.Error("sso: reassign-and-orphan tx failed",
					zap.String("guest_user_id", p.guestUserID),
					zap.String("existing_user_id", existing.ID),
					zap.Error(txErr))
				return nil, txErr
			}
		}
		return existing, nil
	}

	// Step B: try auto-link by verified email (D-03/D-04).
	// SECURITY: only the verifier-derived email reaches this branch — see the
	// AppleSignIn handler's comment for the T-2-EmailBodySpoof rationale.
	//
	// CR-02 fix: the FindUserByVerifiedEmailForLink + Updates pair runs inside
	// db.Transaction so two concurrent sign-ins for the same email (e.g. Apple
	// and Google racing) cannot both pass the read and then both attempt the
	// write — the loser's ErrDuplicate triggers a transactional re-read by
	// provider sub, which now consistently returns either the just-written row
	// (if the loser's sub happens to be the same) or a clean fall-through (if
	// the loser's sub is different — fall through to Step C/D).
	if p.email != "" && p.emailVerified && !p.isPrivateRelay {
		var linkedUser *model.User
		txErr := db.Transaction(func(tx *gorm.DB) error {
			linkCandidate, lerr := repository.FindUserByVerifiedEmailForLink(tx, p.email)
			if lerr != nil {
				if errors.Is(lerr, repository.ErrNotFound) {
					// No candidate — leave linkedUser nil; caller falls through to Step C/D.
					return nil
				}
				return lerr
			}
			updates := map[string]interface{}{
				"auth_provider": p.provider,
			}
			switch p.provider {
			case "apple":
				updates["apple_user_id"] = p.sub
			case "google":
				updates["google_user_id"] = p.sub
			}
			if err := tx.Model(&model.User{}).Where("id = ?", linkCandidate.ID).Updates(updates).Error; err != nil {
				if errors.Is(err, repository.ErrDuplicate) {
					// Race — another caller already wrote a different sub onto
					// this row. Re-read inside the same TX by THIS caller's sub.
					reread, rerr := findUserByProviderID(tx, p.provider, p.sub)
					if rerr != nil {
						if errors.Is(rerr, repository.ErrNotFound) {
							// Our sub doesn't own a row — fall through to Step C/D.
							return nil
						}
						return rerr
					}
					linkedUser = reread
					return nil
				}
				return err
			}
			// Updates succeeded — re-read the merged row inside the TX.
			merged, mrr := repository.FindUserByID(tx, linkCandidate.ID)
			if mrr != nil {
				return mrr
			}
			linkedUser = merged
			return nil
		})
		if txErr != nil {
			return nil, txErr
		}
		if linkedUser != nil {
			return linkedUser, nil
		}
		// linkedUser nil: no candidate found OR race fell through — proceed to Step C/D.
	}

	// Step C: if a guest JWT was presented, promote that row in place.
	if p.guestUserID != "" {
		// WR-04: pass p.fullName so the SSO-supplied display name reaches the
		// users.full_name column on promotion. Empty fullName preserves the
		// existing name (repository guard).
		pErr := repository.PromoteGuestToSSO(db, p.guestUserID, p.sub, p.email, p.provider, p.fullName, p.isPrivateRelay)
		if pErr == nil {
			return repository.FindUserByID(db, p.guestUserID)
		}
		if errors.Is(pErr, repository.ErrDuplicate) {
			// Race lost — another request grabbed this sub. Re-read.
			return findUserByProviderID(db, p.provider, p.sub)
		}
		if !errors.Is(pErr, repository.ErrNotFound) {
			return nil, pErr
		}
		// ErrNotFound — the guest row vanished (race with cleanup). Fall through
		// to Step D and create a brand-new row.
	}

	// Step D: create a new SSO row.
	newUser := &model.User{
		FullName:            p.fullName,
		SubscriptionTier:    "free",
		Role:                "user",
		AuthProvider:        p.provider,
		EmailVerified:       p.emailVerified,
		EmailIsPrivateRelay: p.isPrivateRelay,
	}
	if p.email != "" {
		emailCopy := p.email
		newUser.Email = &emailCopy
	}
	switch p.provider {
	case "apple":
		subCopy := p.sub
		newUser.AppleUserID = &subCopy
	case "google":
		subCopy := p.sub
		newUser.GoogleUserID = &subCopy
	}
	if err := repository.CreateUser(db, newUser); err != nil {
		// W-4: every concurrent caller funnels through this branch — the
		// re-read on ErrDuplicate is what keeps the response 200 instead of 500.
		if errors.Is(err, repository.ErrDuplicate) {
			return findUserByProviderID(db, p.provider, p.sub)
		}
		return nil, err
	}
	// WR-03: a brand-new SSO user (no guest-promotion path, no email-link
	// candidate) must have an active free subscription row so GET
	// /api/v1/subscription returns {plan:"free"} instead of 404. Mirror
	// GuestLogin (handler/auth.go ~ line 458). Failure is non-fatal and
	// logged at WARN — a future repair job can backfill (REVIEW.md WR-03
	// recommended behavior).
	subscription := model.Subscription{
		UserID:   newUser.ID,
		Plan:     "free",
		IsActive: true,
	}
	if err := repository.CreateSubscription(db, &subscription); err != nil {
		logger.Warn("sso: failed to create free subscription for new user (continuing)",
			zap.String("user_id", newUser.ID),
			zap.String("provider", p.provider),
			zap.Error(err))
	}
	return newUser, nil
}

// AppleSignIn handles POST /api/v1/auth/apple.
//
// Flow per D-19 / D-20: parse → verify → resolveSSOUser → generateTokens →
// storeRefreshSession → respond. Optional Authorization: Bearer signals
// guest-promotion intent (D-06).
//
// Error mapping (D-27):
//
//	400 — malformed/missing identityToken
//	401 — verifier error (any kind: sig, aud, exp, iss)
//	403 — invalid guest JWT
//	500 — internal (DB, generateTokens, storeRefreshSession failures)
func AppleSignIn(logger *zap.Logger, cfg *config.Config, db *gorm.DB, verifier appleVerifier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req appleSignInRequest
		if err := c.BodyParser(&req); err != nil || req.IdentityToken == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "identityToken is required"})
		}

		// Optional guest-promotion JWT in Authorization header (T-2-GuestJWTSpoof).
		guestUserID, err := parseGuestJWT(c.Get("Authorization"), cfg.JWTSecret)
		if err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "invalid guest token"})
		}

		identity, err := verifier.Verify(c.Context(), req.IdentityToken)
		if err != nil {
			// HOTFIX-04 contract — single canonical error string, do not leak parser internals.
			logger.Info("apple verify failed", zap.Error(err))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid identity token"})
		}

		// CR-01: a JWT can pass signature/aud/exp/iss verification yet carry no
		// `sub` claim — Apple's JWT library type-asserts a missing claim to "",
		// not an error. Without this guard, resolveSSOUser would create a
		// phantom user row with apple_user_id="" that any future sub-less
		// token would map to. Reject as 401 (same shape as a verifier failure).
		if identity.Sub == "" {
			logger.Warn("apple signin: token missing sub claim")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid identity token"})
		}

		// SECURITY (T-2-EmailBodySpoof): NEVER trust the request body's `email`
		// field as an auto-link key. Auto-link lookup uses `identity.Email` (from
		// the verified JWT). The body `Email` is intentionally ignored for any
		// trust-bearing lookup; it is never passed to FindUserByVerifiedEmailForLink.
		_ = req.Email

		user, err := resolveSSOUser(db, logger, ssoResolveParams{
			provider:       "apple",
			sub:            identity.Sub,
			email:          identity.Email,
			emailVerified:  identity.EmailVerified,
			isPrivateRelay: identity.IsPrivateRelay,
			fullName:       req.FullName,
			guestUserID:    guestUserID,
		})
		if err != nil {
			logger.Error("apple signin: resolve user", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Phase 3 D-29: backfill plan_id for SSO users created without one
		// (resolveSSOUser doesn't set it). New rows from Step D, guest-promotions,
		// and email-auto-linked rows may all land with empty plan_id; fill from
		// FindSystemPlanID so the JWT carries the claim. Failure is non-fatal —
		// middleware fallback covers it.
		if user.PlanID == "" {
			if systemPlanID, sysErr := repository.FindSystemPlanID(db); sysErr == nil && systemPlanID != "" {
				if uErr := db.Model(&model.User{}).Where("id = ?", user.ID).Update("plan_id", systemPlanID).Error; uErr == nil {
					user.PlanID = systemPlanID
				} else {
					logger.Warn("apple signin: failed to set system plan_id (continuing)",
						zap.String("user_id", user.ID), zap.Error(uErr))
				}
			}
		}

		tokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, user.PlanID, cfg.JWTSecret)
		if err != nil {
			logger.Error("apple signin: generate tokens", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
			logger.Error("apple signin: store refresh", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		return c.JSON(fiber.Map{"data": ssoResponseBody(user, tokens)})
	}
}

// GoogleSignIn handles POST /api/v1/auth/google. Same composition as AppleSignIn.
func GoogleSignIn(logger *zap.Logger, cfg *config.Config, db *gorm.DB, verifier googleVerifier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req googleSignInRequest
		if err := c.BodyParser(&req); err != nil || req.IDToken == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "idToken is required"})
		}

		guestUserID, err := parseGuestJWT(c.Get("Authorization"), cfg.JWTSecret)
		if err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "invalid guest token"})
		}

		identity, err := verifier.Verify(c.Context(), req.IDToken)
		if err != nil {
			logger.Info("google verify failed", zap.Error(err))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid identity token"})
		}

		// CR-01: same empty-sub guard as AppleSignIn — see that handler's
		// comment for the full rationale (REVIEW.md CR-01 / VERIFICATION.md truth #1).
		if identity.Sub == "" {
			logger.Warn("google signin: token missing sub claim")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid identity token"})
		}

		user, err := resolveSSOUser(db, logger, ssoResolveParams{
			provider:       "google",
			sub:            identity.Sub,
			email:          identity.Email,
			emailVerified:  identity.EmailVerified,
			isPrivateRelay: false, // Google has no private-relay concept
			guestUserID:    guestUserID,
		})
		if err != nil {
			logger.Error("google signin: resolve user", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Phase 3 D-29: same plan_id backfill as AppleSignIn. See comment there.
		if user.PlanID == "" {
			if systemPlanID, sysErr := repository.FindSystemPlanID(db); sysErr == nil && systemPlanID != "" {
				if uErr := db.Model(&model.User{}).Where("id = ?", user.ID).Update("plan_id", systemPlanID).Error; uErr == nil {
					user.PlanID = systemPlanID
				} else {
					logger.Warn("google signin: failed to set system plan_id (continuing)",
						zap.String("user_id", user.ID), zap.Error(uErr))
				}
			}
		}

		tokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, user.PlanID, cfg.JWTSecret)
		if err != nil {
			logger.Error("google signin: generate tokens", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
			logger.Error("google signin: store refresh", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": ssoResponseBody(user, tokens)})
	}
}

// ssoResponseBody builds the `data` portion of the SSO response (D-21).
// Identical shape to the existing GuestLogin/AdminLogin response payload.
func ssoResponseBody(user *model.User, tokens *authResponse) fiber.Map {
	email := ""
	if user.Email != nil {
		email = *user.Email
	}
	return fiber.Map{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
		"user": fiber.Map{
			"id":                user.ID,
			"auth_provider":     user.AuthProvider,
			"email":             email,
			"full_name":         user.FullName,
			"subscription_tier": user.SubscriptionTier,
		},
	}
}

// --- Phase 2 Logout (AUTH-08) -----------------------------------------------

// Logout handles POST /api/v1/auth/logout.
//
// Mounted under the protected group in cmd/main.go — the AuthRequired
// middleware (HOTFIX-02) validates the JWT and sets c.Locals("user_id")
// BEFORE this handler runs. The middleware also already checks
// cache.IsTokenBlacklisted on every protected request, so the blacklist
// SET below makes subsequent requests with the same access token return
// 401 automatically — no middleware surgery needed.
//
// Behaviour (D-23, D-24):
//  1. Delete ALL sessions for the user (Discretion default per RESEARCH.md
//     §Open Question #1 recommendation a; matches "logout means logout").
//  2. Blacklist the access-token's SHA-256 hash with TTL = min(exp-now, 5min).
//  3. Return 204 No Content.
//
// Errors are mapped per HOTFIX-04: generic 500 on session-delete failure,
// 204 on everything else (blacklist write is fail-open per IsTokenBlacklisted
// contract — Redis outage does not block logout).
//
// Blacklist key prefix divergence: the IN-TREE prefix at
// internal/cache/redis.go:35 is "token:blacklist:", NOT CONTEXT.md D-24's
// proposed value. This handler calls cache.BlacklistToken so the prefix is
// owned by ONE constant — there is no way for the writer (here) and reader
// (middleware/auth.go's IsTokenBlacklisted call) to drift. See plan 02-06
// objective and plan 02-07 contract doc for the documented rationale.
func Logout(logger *zap.Logger, redisClient *redis.Client, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			// The middleware should have set this; defensive check guards
			// against accidental mounting under the public group.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		// Step 1: delete sessions. Postgres write — fails loud. Ordering
		// matters: deleting sessions BEFORE blacklisting the access token
		// means a partial failure leaves the user "still able to use their
		// access token for ≤5min" rather than "still able to mint new
		// access tokens via refresh forever" — the milder failure mode.
		if _, err := repository.DeleteUserSessions(db, userID); err != nil {
			logger.Error("logout: delete sessions", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Step 2: blacklist the access token. Redis write — fails open
		// (per cache.IsTokenBlacklisted's fail-open contract).
		authHeader := c.Get("Authorization")
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString != "" && redisClient != nil {
			// Decode claims without re-verifying — the middleware already
			// verified the signature; we only need `exp` for TTL clamp.
			parser := jwt.NewParser(jwt.WithoutClaimsValidation())
			claims := jwt.MapClaims{}
			_, _, _ = parser.ParseUnverified(tokenString, claims)
			var ttl time.Duration
			if exp, ok := claims["exp"].(float64); ok {
				ttl = time.Until(time.Unix(int64(exp), 0))
				if ttl > 5*time.Minute {
					ttl = 5 * time.Minute // clamp per D-24
				}
				if ttl < 0 {
					ttl = 0
				}
			}
			// WR-02: use `ttl >= 0` so the boundary case (token expiring this
			// exact second) still produces an audit-trail entry in Redis. Per
			// REVIEW.md WR-02: even a near-zero TTL keeps the keyspace
			// observer's record complete.
			if ttl >= 0 {
				tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenString)))
				if err := cache.BlacklistToken(c.Context(), redisClient, tokenHash, ttl); err != nil {
					logger.Warn("logout: blacklist write failed (fail-open)",
						zap.String("user_id", userID), zap.Error(err))
				}
			}
		}

		return c.SendStatus(fiber.StatusNoContent)
	}
}
