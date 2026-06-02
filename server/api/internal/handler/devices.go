package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Sentinels returned from inside the LinkDevice transaction so the outer
// HTTP layer can map them to the right status code without flattening
// every transaction error to 500.
var (
	// errDeviceLimitReached: owner is at their plan's device cap → 403
	errDeviceLimitReached = errors.New("device limit reached")

	// errDeviceClaimedBySomeoneElse: a row already exists for this device_id,
	// it is bound to a different user, and the redeeming client did not
	// present that user's secret. We refuse to silently steal the row → 403.
	errDeviceClaimedBySomeoneElse = errors.New("device already claimed")

	// errOwnerMissing: the owner referenced by the link code no longer
	// exists. Should be impossible given the FK, but if it happens we want
	// the response to be 500 not 404 — preserves observability.
	errOwnerMissing = errors.New("owner missing")
)

// generateLinkCode returns a 6-digit zero-padded numeric code.
// Uses crypto/rand so codes are unguessable.
func generateLinkCode() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return padCode(n.Int64()), nil
}

// padCode returns the 6-digit zero-padded decimal representation of n.
// The slice is pre-initialised with six '0' bytes; the loop fills digits
// from the least significant end and exits as soon as n reaches 0, leaving
// any leading positions as '0'. padCode(0) therefore returns "000000".
// Inputs >= 1_000_000 are truncated to their last 6 digits — generateLinkCode
// guarantees this never happens because rand.Int's upper bound is 1_000_000.
func padCode(n int64) string {
	s := []byte{'0', '0', '0', '0', '0', '0'}
	for i := 5; i >= 0 && n > 0; i-- {
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s)
}

// CreateShareCode handles POST /devices/share-code.
// Generates a one-time, short-lived 6-digit code that a friend's device can
// redeem via /auth/link to attach itself to the caller's account.
//
// Refuses if the caller already has an unexpired code outstanding (one
// in-flight share at a time per user) or if their device cap is already at
// the maximum (no point sharing if there is no slot to fill).
//
// The code lifetime is taken from cfg.LinkCodeTTL so deployments can tune
// it without redeploying — short enough to defeat brute force, long enough
// to dictate over a phone call.
func CreateShareCode(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		// Refuse if a code is already outstanding for this user.
		active, err := repository.CountActiveLinkCodesForUser(c.Context(), db, userID)
		if err != nil {
			logger.Error("share-code: count active failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		if active > 0 {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "a share code is already active for this account",
			})
		}

		// Refuse if the user's device cap leaves no room for an additional device.
		user, err := repository.FindUserByID(c.Context(), db, userID)
		if err != nil {
			logger.Error("share-code: load user failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		// PAY-11 / D-24: read device cap from the plan row via the user's plan_id.
		plan, perr := repository.FindPlanByID(c.Context(), db, user.PlanID)
		if perr != nil {
			logger.Warn("CreateShareCode: FindPlanByID failed; falling back to system plan",
				zap.String("plan_id", user.PlanID), zap.Error(perr))
			systemPlanID, sperr := repository.FindSystemPlanID(c.Context(), db)
			if sperr != nil {
				logger.Error("CreateShareCode: FindSystemPlanID failed", zap.Error(sperr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			plan, sperr = repository.FindPlanByID(c.Context(), db, systemPlanID)
			if sperr != nil {
				logger.Error("CreateShareCode: FindPlanByID(system) failed", zap.Error(sperr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
		}
		limits := struct {
			MaxDevices int
			MaxServers int
		}{MaxDevices: plan.MaxDevices, MaxServers: plan.MaxServers}
		if limits.MaxDevices != model.UnlimitedDevices {
			deviceCount, err := repository.CountDevicesByUser(c.Context(), db, userID)
			if err != nil {
				logger.Error("share-code: count devices failed", zap.Error(err))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			if deviceCount >= int64(limits.MaxDevices) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":       "device limit already reached on this plan",
					"max_devices": limits.MaxDevices,
				})
			}
		}

		// Generate a unique code, retrying on the rare collision (1 in 10^6).
		// More than one retry signals code-space pressure (someone is sharing
		// at scale, or the table has too many active codes); operators should
		// investigate if this fires regularly.
		var code string
		for attempt := 0; attempt < 5; attempt++ {
			if attempt > 0 {
				logger.Warn("share-code: collision retry",
					zap.Int("attempt", attempt),
					zap.String("user_id", userID),
				)
			}
			candidate, err := generateLinkCode()
			if err != nil {
				logger.Error("share-code: rng failed", zap.Error(err))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			lc := &model.LinkCode{
				Code:      candidate,
				UserID:    userID,
				ExpiresAt: time.Now().Add(cfg.LinkCodeTTL),
			}
			if err := repository.CreateLinkCode(c.Context(), db, lc); err != nil {
				if errors.Is(err, repository.ErrDuplicate) {
					continue // try again with a different number
				}
				logger.Error("share-code: insert failed", zap.Error(err))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			code = candidate
			break
		}
		if code == "" {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "could not allocate a unique code",
			})
		}

		logger.Info("share-code created",
			zap.String("user_id", userID),
			zap.Int("expires_in_sec", int(cfg.LinkCodeTTL.Seconds())),
		)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": fiber.Map{
				"code":           code,
				"expires_in_sec": int(cfg.LinkCodeTTL.Seconds()),
			},
		})
	}
}

type linkRequest struct {
	Code         string `json:"code"`
	DeviceID     string `json:"device_id"`
	DeviceSecret string `json:"device_secret"`
	Platform     string `json:"platform"`
	Model        string `json:"model"`
}

// LinkDevice handles POST /auth/link.
//
// The caller is a brand-new device that holds a 6-digit code given out by an
// existing plan owner. The whole flow runs in a single database transaction
// so that the code consume, the cap check, and the device reassignment can
// not race against a concurrent redemption of a different code for the same
// owner — which would otherwise let the owner's quota be exceeded.
//
// Within the transaction:
//   1. Consume the code (one-time use, fails if expired/missing).
//   2. Load the owner row and check the cap, taking into account whether
//      the redeeming device is already bound to the owner (link replay).
//   3. Reassign the redeeming device row to the owner (or create it if
//      this is a brand-new device_id).
//   4. If the previous owner of the device row was an unused anonymous
//      guest, delete that user — its subscription/sessions cascade.
//
// Outside the transaction:
//   5. Issue fresh JWT tokens for the owner.
//
// This endpoint is intentionally NOT auth-protected — the caller is exactly
// the unauthenticated guest device that wants to attach to a paid account.
// The version-gate middleware still requires X-App-Version, so old APKs
// cannot reach this endpoint, and the dedicated per-IP rate limit on
// /auth/link bounds brute-force attempts on the 6-digit code space.
func LinkDevice(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req linkRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}
		if req.Code == "" || req.DeviceID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "code and device_id are required",
			})
		}

		secretHash := hashDeviceSecret(req.DeviceSecret)

		// Outputs of the transaction that the response handler needs.
		var owner *model.User
		var orphanedUserID string

		txErr := db.Transaction(func(tx *gorm.DB) error {
			// 1. Consume the code atomically. The repository helper itself
			//    runs a sub-transaction; nesting is fine because GORM will
			//    re-use the outer transaction's connection.
			lc, err := repository.ConsumeLinkCode(c.Context(), tx, req.Code)
			if err != nil {
				return err
			}

			// 2. Load the owner row.
			loaded, err := repository.FindUserByID(c.Context(), tx, lc.UserID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					return errOwnerMissing
				}
				return fmt.Errorf("link: owner lookup: %w", err)
			}
			owner = loaded

			// PAY-11 / D-24: read device cap from the owner's plan row via the tx.
			plan, perr := repository.FindPlanByID(c.Context(), tx, owner.PlanID)
			if perr != nil {
				logger.Warn("LinkDevice: FindPlanByID failed; falling back to system plan",
					zap.String("plan_id", owner.PlanID), zap.Error(perr))
				systemPlanID, sperr := repository.FindSystemPlanID(c.Context(), tx)
				if sperr != nil {
					return fmt.Errorf("link: find system plan: %w", sperr)
				}
				plan, sperr = repository.FindPlanByID(c.Context(), tx, systemPlanID)
				if sperr != nil {
					return fmt.Errorf("link: find system plan row: %w", sperr)
				}
			}
			limits := struct {
				MaxDevices int
				MaxServers int
			}{MaxDevices: plan.MaxDevices, MaxServers: plan.MaxServers}

			// 3. Capacity check inside the transaction. The redeeming
			//    device is treated as already-counted if it is currently
			//    bound to the owner (link replay): otherwise we'd double
			//    count it after the reassign/insert below.
			var existingDevice *model.Device
			if d, err := repository.FindDeviceByDeviceID(c.Context(), tx, req.DeviceID); err == nil {
				existingDevice = d
			} else if !errors.Is(err, repository.ErrNotFound) {
				return fmt.Errorf("link: lookup existing device: %w", err)
			}

			// 3a. SECURITY CHECK: if a row exists for this device_id and it
			//     is owned by someone OTHER than the owner of this code, the
			//     redeeming client must prove ownership by presenting the
			//     existing row's secret. Otherwise the link flow would let
			//     a device_id leak become an account-rebind primitive.
			//
			//     The "legacy migration" exception applies: if the existing
			//     row has no secret on file (created before migration 012),
			//     accept the link as a one-time grace upgrade.
			if existingDevice != nil && existingDevice.UserID != owner.ID {
				if existingDevice.DeviceSecretHash != "" &&
					subtle.ConstantTimeCompare([]byte(existingDevice.DeviceSecretHash), []byte(secretHash)) != 1 {
					return errDeviceClaimedBySomeoneElse
				}
			}

			if limits.MaxDevices != model.UnlimitedDevices {
				count, err := repository.CountDevicesByUser(c.Context(), tx, owner.ID)
				if err != nil {
					return fmt.Errorf("link: count devices: %w", err)
				}
				alreadyBoundToOwner := existingDevice != nil && existingDevice.UserID == owner.ID
				if !alreadyBoundToOwner && count >= int64(limits.MaxDevices) {
					return errDeviceLimitReached
				}
			}

			// 4. Reassign or create the device row.
			if existingDevice != nil {
				// Track the previous owner so we can clean up the orphan
				// guest user after the transaction commits.
				if existingDevice.UserID != owner.ID {
					orphanedUserID = existingDevice.UserID
				}
				if err := repository.ReassignDeviceUser(c.Context(), tx, req.DeviceID, owner.ID, req.Platform, req.Model, secretHash); err != nil {
					return fmt.Errorf("link: reassign device: %w", err)
				}
			} else {
				device := model.Device{
					UserID:           owner.ID,
					DeviceID:         req.DeviceID,
					DeviceSecretHash: secretHash,
					Platform:         req.Platform,
					Model:            req.Model,
				}
				if err := repository.CreateDevice(c.Context(), tx, &device); err != nil {
					return fmt.Errorf("link: create device: %w", err)
				}
			}

			return nil
		})

		if txErr != nil {
			ownerLogID := ""
			if owner != nil {
				ownerLogID = owner.ID
			}
			switch {
			case errors.Is(txErr, repository.ErrNotFound):
				logger.Warn("link: invalid or expired code",
					zap.String("device_id", req.DeviceID),
				)
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "invalid or expired code",
				})
			case errors.Is(txErr, errDeviceLimitReached):
				logger.Warn("link: owner device cap reached",
					zap.String("device_id", req.DeviceID),
					zap.String("owner_user_id", ownerLogID),
				)
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "owner's device limit reached",
				})
			case errors.Is(txErr, errDeviceClaimedBySomeoneElse):
				logger.Warn("link: device_id already claimed by another user",
					zap.String("device_id", req.DeviceID),
					zap.String("owner_user_id", ownerLogID),
				)
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "this device is already attached to another account",
				})
			case errors.Is(txErr, errOwnerMissing):
				// Distinct from "code expired" — code consumed but owner row
				// vanished. Should be impossible given the FK; logged as
				// error so observability picks it up.
				logger.Error("link: owner missing despite valid code",
					zap.String("device_id", req.DeviceID),
				)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			default:
				logger.Error("link: transaction failed",
					zap.String("device_id", req.DeviceID),
					zap.Error(txErr),
				)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
		}

		// Best-effort orphan cleanup outside the transaction. If the device
		// used to belong to an unused anonymous guest with no other devices
		// and no email, drop it so the users table doesn't accumulate
		// shells. Failure here is non-fatal; the cleanup scheduler can
		// pick up the orphan later.
		if orphanedUserID != "" && orphanedUserID != owner.ID {
			if err := repository.DeleteOrphanGuestUser(c.Context(), db, orphanedUserID); err != nil && !errors.Is(err, repository.ErrNotFound) {
				logger.Warn("link: orphan cleanup failed",
					zap.String("orphan_user_id", orphanedUserID),
					zap.Error(err),
				)
			}
		}

		// Issue tokens that authenticate the caller as the owner.
		tokens, err := generateTokens(owner.ID, owner.SubscriptionTier, owner.Role, owner.FullName, owner.PlanID, cfg.JWTSecret)
		if err != nil {
			logger.Error("link: token generation failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		// HARD-04: bind the session to the linking device + issue IP.
		_ = storeRefreshSession(c.Context(), db, owner.ID, tokens.RefreshToken, req.DeviceID, c.IP())

		logger.Info("device linked to owner",
			zap.String("owner_user_id", owner.ID),
			zap.String("device_id", req.DeviceID),
		)

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"data": tokens,
		})
	}
}

// ListMyDevices handles GET /devices.
// Returns the list of devices currently bound to the calling user.
func ListMyDevices(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)
		devices, err := repository.ListDevicesByUser(c.Context(), db, userID)
		if err != nil {
			logger.Error("list-devices failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		return c.JSON(fiber.Map{"data": devices})
	}
}

// DeleteMyDevice handles DELETE /devices/:id.
//
// Removes one device row from the calling user's quota. Used by the plan
// owner to evict a slot occupied by a device that has gone away (lost
// phone, factory reset, iOS reinstall that generated a new IDFV, friend
// who never came back).
//
// Authorisation: a user can only delete devices they currently own.
// The repository check enforces this — passing someone else's device id
// returns 404 just like a non-existent id.
func DeleteMyDevice(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)
		deviceRowID := c.Params("id")
		if deviceRowID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "device id required",
			})
		}

		if err := repository.DeleteDeviceByOwner(c.Context(), db, deviceRowID, userID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "device not found",
				})
			}
			logger.Error("delete-device failed",
				zap.String("user_id", userID),
				zap.String("device_row_id", deviceRowID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Info("device removed",
			zap.String("user_id", userID),
			zap.String("device_row_id", deviceRowID),
		)
		return c.SendStatus(fiber.StatusNoContent)
	}
}
