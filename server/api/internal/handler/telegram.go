package handler

import (
	"errors"
	"fmt"

	"vpnapp/server/api/internal/recovery"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// botUsername is injected at startup from config (see cmd/main.go).
// Handlers read it to build deep-link URLs. Kept as a package-level
// variable rather than a config field because the handler closures
// are created before the bot's getMe call completes, but the deep
// links are only rendered after the first request — this lets
// main.go update the value once the bot has announced itself.
var telegramBotUsername = "risevp_bot"

// SetTelegramBotUsername lets the bot package register its own
// username once it has been fetched from the Telegram API.
// Called from cmd/main.go after the bot starts.
func SetTelegramBotUsername(u string) { telegramBotUsername = u }

// TelegramLinkIntent handles POST /auth/telegram/link-intent.
// The caller must be an authenticated VPN user. Returns a
// short-lived deep link pointing at the recovery bot's /start
// endpoint with a Redis-backed opaque token encoded in the
// payload. The mobile app opens the URL via Linking.openURL and
// the bot takes over from there.
//
// Token format is critical: Telegram's /start payload is capped
// at 64 characters and only accepts [A-Za-z0-9_-]. JWTs exceed
// both limits (they contain "." and average ~200 chars), so we
// mint a 32-char opaque token stored in Redis instead of a
// signed JWT. See recovery/start_token.go for details.
func TelegramLinkIntent(logger *zap.Logger, rdb *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}
		token, err := recovery.MintStartToken(c.Context(), rdb, userID, recovery.PurposeLink)
		if err != nil {
			logger.Error("telegram: link-intent mint failed",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		url := fmt.Sprintf("https://t.me/%s?start=link_%s", telegramBotUsername, token)
		logger.Info("telegram: link intent issued", zap.String("user_id", userID))
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"url":     url,
				"expires": int(recovery.StartTokenTTL.Seconds()),
			},
		})
	}
}

// TelegramRestoreIntent handles POST /auth/telegram/restore-intent.
// The caller is authenticated as a *new* guest user — the one just
// created on a fresh install. Returns a deep link with a restore
// start token whose value points to the new guest's user id. The
// bot takes from.id from the /start sender, looks up the old user
// via FindUserByTelegramID, and runs PerformRestore(old, new, tgID).
func TelegramRestoreIntent(logger *zap.Logger, rdb *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}
		token, err := recovery.MintStartToken(c.Context(), rdb, userID, recovery.PurposeRestore)
		if err != nil {
			logger.Error("telegram: restore-intent mint failed",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		url := fmt.Sprintf("https://t.me/%s?start=restore_%s", telegramBotUsername, token)
		logger.Info("telegram: restore intent issued", zap.String("user_id", userID))
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"url":     url,
				"expires": int(recovery.StartTokenTTL.Seconds()),
			},
		})
	}
}

// TelegramStatus handles GET /account/telegram-status.
// Returns whether the calling user has a Telegram recovery
// binding and when it was established. Used by the mobile Account
// screen to render "Привязано ✓" with the link date.
func TelegramStatus(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}
		user, err := repository.FindUserByID(c.Context(), db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("telegram: status lookup failed",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		linked := user.TelegramUserID != nil
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"linked":              linked,
				"linked_at":           user.TelegramLinkedAt,
				"telegram_username":   user.TelegramUsername,
				"telegram_first_name": user.TelegramFirstName,
			},
		})
	}
}

// TelegramUnlink handles DELETE /account/telegram.
// Clears the Telegram binding on the calling user's row so they can
// re-link to a different Telegram account (ADR-006 open question
// #5: re-linking allowed). Idempotent — unlinking a never-linked
// user returns 204 without error so the mobile app doesn't need
// to check state first.
func TelegramUnlink(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}
		if err := repository.UnlinkTelegramAccount(c.Context(), db, userID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.SendStatus(fiber.StatusNoContent)
			}
			logger.Error("telegram: unlink failed",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		logger.Info("telegram: unlinked", zap.String("user_id", userID))
		return c.SendStatus(fiber.StatusNoContent)
	}
}
