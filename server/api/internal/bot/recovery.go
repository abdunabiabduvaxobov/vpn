// Package bot implements the Telegram recovery bot for ADR-006.
//
// The bot runs as a goroutine inside the API server process. It
// long-polls the Telegram Bot API for updates and handles a small
// set of /start deep-link payloads:
//
//   - /start link_<jwt>    — bind a Telegram user id to the VPN user
//                            id encoded in the JWT.
//   - /start restore_<jwt> — rebind the device that just got a fresh
//                            guest account to the previously-linked
//                            VPN user owned by this Telegram account.
//
// All other messages (plain /start, /help, /status, stickers, voice,
// photos, text) get ignored silently. ADR-006 open question #4 was
// resolved as "silent" during design review.
//
// The bot talks to the database directly via the repository package
// — no HTTP round-trip back to the API server, since the whole thing
// lives in the same process.
package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/recovery"
	"vpnapp/server/api/internal/repository"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Recovery is the top-level bot handle. Created once at API startup
// and driven by Run until the context is cancelled.
type Recovery struct {
	api    *tgbotapi.BotAPI
	db     *gorm.DB
	rdb    *redis.Client
	logger *zap.Logger
	cfg    *config.Config

	// pendingRestores tracks restore requests awaiting the user's
	// Yes/No confirmation via inline keyboard callback. Keyed by
	// (chatID, messageID) so collision across users is impossible.
	// A tiny in-memory map is enough because these entries are
	// created and consumed in the same bot goroutine and cleared
	// within seconds.
	pendingRestores map[string]pendingRestore
}

type pendingRestore struct {
	oldUserID string
	newUserID string
	tgUserID  int64
	expiresAt time.Time
}

// New constructs a recovery bot from config. Returns nil, nil when
// the recovery bot token is not set — the caller treats that as a
// deliberate "disabled" state and does not start a goroutine.
//
// Returns (nil, err) only for genuine setup failures (bad token,
// network unreachable at startup). A network error at startup is
// still fatal because we want loud failures during deploy, not a
// silent dead bot.
func New(cfg *config.Config, db *gorm.DB, rdb *redis.Client, logger *zap.Logger) (*Recovery, error) {
	if cfg.RecoveryBotToken == "" {
		return nil, nil
	}
	if rdb == nil {
		return nil, fmt.Errorf("bot: redis client is required for start-token lookup")
	}
	api, err := tgbotapi.NewBotAPI(cfg.RecoveryBotToken)
	if err != nil {
		return nil, fmt.Errorf("bot: authenticate with Telegram: %w", err)
	}
	logger.Info("telegram recovery bot authenticated",
		zap.String("username", api.Self.UserName),
		zap.Int64("id", api.Self.ID),
	)
	// If the username in config is wrong, trust what Telegram
	// reports — deep links must match the live bot or /start
	// payloads will land on the wrong bot.
	if api.Self.UserName != "" && cfg.RecoveryBotUsername != api.Self.UserName {
		logger.Warn("telegram recovery bot username mismatch",
			zap.String("config", cfg.RecoveryBotUsername),
			zap.String("actual", api.Self.UserName),
		)
		cfg.RecoveryBotUsername = api.Self.UserName
	}
	return &Recovery{
		api:             api,
		db:              db,
		rdb:             rdb,
		logger:          logger,
		cfg:             cfg,
		pendingRestores: make(map[string]pendingRestore),
	}, nil
}

// Run starts the long-poll loop and blocks until ctx is cancelled.
// Safe to call as a goroutine. Errors from individual updates are
// logged and swallowed so one bad message cannot stop the loop.
func (r *Recovery) Run(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30 // long-poll timeout in seconds
	u.AllowedUpdates = []string{"message", "callback_query"}

	updates := r.api.GetUpdatesChan(u)
	r.logger.Info("telegram recovery bot polling",
		zap.String("username", r.api.Self.UserName),
	)

	for {
		select {
		case <-ctx.Done():
			r.api.StopReceivingUpdates()
			r.logger.Info("telegram recovery bot stopped")
			return
		case upd, ok := <-updates:
			if !ok {
				r.logger.Warn("telegram recovery bot updates channel closed")
				return
			}
			r.handleUpdate(ctx, upd)
			r.gcPendingRestores()
		}
	}
}

// handleUpdate routes a single update to the right handler. Never
// panics — any error bubbled up here is logged and swallowed so the
// polling loop keeps running. Only /start commands (with and
// without payloads) and callback queries get any reply at all;
// everything else is silently ignored per the ADR.
func (r *Recovery) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("telegram recovery bot handler panic",
				zap.Any("recover", rec),
			)
		}
	}()

	if upd.CallbackQuery != nil {
		r.handleCallback(ctx, upd.CallbackQuery)
		return
	}
	msg := upd.Message
	if msg == nil || msg.From == nil {
		return
	}
	// Commands only — ignore plain text, stickers, voice, photos.
	if !msg.IsCommand() {
		return
	}
	switch msg.Command() {
	case "start":
		r.handleStart(ctx, msg)
	case "help":
		r.sendHelp(msg.Chat.ID)
	case "status":
		r.sendStatus(ctx, msg)
	default:
		// Silent on unknown commands too.
	}
}

// handleStart dispatches /start commands with and without payloads.
// Bare /start is a welcome message. /start link_<jwt> and
// /start restore_<jwt> run the two real flows.
func (r *Recovery) handleStart(ctx context.Context, msg *tgbotapi.Message) {
	payload := strings.TrimSpace(msg.CommandArguments())
	if payload == "" {
		r.sendHelp(msg.Chat.ID)
		return
	}
	switch {
	case strings.HasPrefix(payload, "link_"):
		r.handleLink(ctx, msg, strings.TrimPrefix(payload, "link_"))
	case strings.HasPrefix(payload, "restore_"):
		r.handleRestore(ctx, msg, strings.TrimPrefix(payload, "restore_"))
	default:
		r.sendHelp(msg.Chat.ID)
	}
}

// handleLink consumes a link start token from Redis and binds the
// sender's Telegram id to the VPN user id it references.
func (r *Recovery) handleLink(ctx context.Context, msg *tgbotapi.Message, token string) {
	logger := r.logger.With(
		zap.Int64("tg_user_id", msg.From.ID),
		zap.String("tg_username", msg.From.UserName),
	)
	payload, err := recovery.ConsumeStartToken(ctx, r.rdb, token)
	if err != nil {
		logger.Warn("telegram recovery bot: invalid link token", zap.Error(err))
		r.reply(msg.Chat.ID, "❌ Ссылка истекла или недействительна. Откройте приложение и попробуйте ещё раз.")
		return
	}
	if payload.Purpose != recovery.PurposeLink {
		logger.Warn("telegram recovery bot: link endpoint called with non-link token")
		r.reply(msg.Chat.ID, "❌ Неверный тип ссылки.")
		return
	}
	if err := repository.LinkTelegramAccount(
		ctx,
		r.db,
		payload.Subject,
		msg.From.ID,
		msg.From.UserName,
		msg.From.FirstName,
	); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			r.reply(msg.Chat.ID, "❌ Аккаунт VPN не найден. Попробуйте войти в приложение заново.")
			return
		}
		if errors.Is(err, repository.ErrDuplicate) {
			// The VPN user already has a binding to a *different*
			// Telegram account. Refuse and tell the user to unlink
			// first via the app.
			r.reply(msg.Chat.ID, "❌ Этот аккаунт уже привязан к другому Telegram. Откройте приложение и нажмите «Отвязать», затем повторите.")
			return
		}
		logger.Error("telegram recovery bot: LinkTelegramAccount failed", zap.Error(err))
		r.reply(msg.Chat.ID, "⚠️ Временная ошибка, попробуйте через минуту.")
		return
	}
	logger.Info("telegram recovery bot: linked",
		zap.String("user_id", payload.Subject),
	)
	r.writeAudit(ctx, "tg_link", payload.Subject, msg.From.ID, msg.Chat.ID)
	r.reply(msg.Chat.ID,
		"✅ Аккаунт VPN привязан к этому Telegram.\n\n"+
			"Теперь вы можете восстановить подписку на любом новом устройстве "+
			"через кнопку «Восстановить через Telegram» в приложении.",
	)
}

// handleRestore consumes a restore start token, looks up the old
// user by the sender's Telegram id, and shows an inline keyboard
// asking the user to confirm the merge. The actual merge happens
// in handleCallback when the user taps Yes.
func (r *Recovery) handleRestore(ctx context.Context, msg *tgbotapi.Message, token string) {
	logger := r.logger.With(zap.Int64("tg_user_id", msg.From.ID))
	payload, err := recovery.ConsumeStartToken(ctx, r.rdb, token)
	if err != nil {
		logger.Warn("telegram recovery bot: invalid restore token", zap.Error(err))
		r.reply(msg.Chat.ID, "❌ Ссылка истекла. Откройте приложение и нажмите «Восстановить через Telegram» заново.")
		return
	}
	if payload.Purpose != recovery.PurposeRestore {
		logger.Warn("telegram recovery bot: restore endpoint called with non-restore token")
		r.reply(msg.Chat.ID, "❌ Неверный тип ссылки.")
		return
	}
	newUserID := payload.Subject

	oldUser, err := repository.FindUserByTelegramID(ctx, r.db, msg.From.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			r.reply(msg.Chat.ID,
				"❌ Этот Telegram не привязан ни к одному VPN-аккаунту.\n\n"+
					"Если у вас остался доступ к старому устройству — привяжите "+
					"Telegram через раздел «Аккаунт» в приложении, а затем повторите "+
					"восстановление. Если доступа уже нет — напишите @flawlssr.",
			)
			return
		}
		logger.Error("telegram recovery bot: FindUserByTelegramID failed", zap.Error(err))
		r.reply(msg.Chat.ID, "⚠️ Временная ошибка, попробуйте через минуту.")
		return
	}

	if oldUser.ID == newUserID {
		r.reply(msg.Chat.ID, "ℹ️ Этот аккаунт уже привязан к этому устройству.")
		return
	}

	// Show a confirmation prompt with Yes/No inline buttons. The
	// merge only runs after the user taps Yes — this protects
	// against an attacker who intercepts a restore deep link and
	// forwards it to the victim to accidentally click.
	shortOld := oldUser.ID
	if len(shortOld) > 8 {
		shortOld = shortOld[:8]
	}
	text := fmt.Sprintf(
		"Восстановить подписку на это устройство?\n\n"+
			"Старый аккаунт: <code>%s…</code>\n"+
			"Новый девайс свяжется с этой учётной записью, а его временный "+
			"гостевой профиль будет удалён.",
		shortOld,
	)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, восстановить", "restore_yes"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "restore_no"),
		),
	)
	sent := tgbotapi.NewMessage(msg.Chat.ID, text)
	sent.ParseMode = "HTML"
	sent.ReplyMarkup = keyboard
	m, err := r.api.Send(sent)
	if err != nil {
		logger.Error("telegram recovery bot: send confirmation failed", zap.Error(err))
		return
	}

	key := fmt.Sprintf("%d:%d", m.Chat.ID, m.MessageID)
	r.pendingRestores[key] = pendingRestore{
		oldUserID: oldUser.ID,
		newUserID: newUserID,
		tgUserID:  msg.From.ID,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
}

// handleCallback resolves the confirmation prompt from handleRestore.
// Runs PerformRestore on Yes, otherwise drops the pending entry and
// posts a cancellation notice.
func (r *Recovery) handleCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	if cq == nil || cq.Message == nil || cq.From == nil {
		return
	}
	key := fmt.Sprintf("%d:%d", cq.Message.Chat.ID, cq.Message.MessageID)
	pending, ok := r.pendingRestores[key]
	if !ok {
		// Expired or replayed callback — acknowledge and move on.
		_, _ = r.api.Request(tgbotapi.NewCallback(cq.ID, "Истекло"))
		return
	}
	if time.Now().After(pending.expiresAt) {
		delete(r.pendingRestores, key)
		_, _ = r.api.Request(tgbotapi.NewCallback(cq.ID, "Истекло"))
		r.editMessage(cq.Message, "⏱️ Подтверждение истекло. Откройте приложение и повторите.")
		return
	}
	if cq.From.ID != pending.tgUserID {
		// Only the original requester can answer. A curious member
		// of a group can't confirm someone else's restore.
		_, _ = r.api.Request(tgbotapi.NewCallback(cq.ID, "Только отправитель может подтвердить"))
		return
	}

	delete(r.pendingRestores, key)
	_, _ = r.api.Request(tgbotapi.NewCallback(cq.ID, ""))

	if cq.Data == "restore_no" {
		r.editMessage(cq.Message, "Отменено.")
		return
	}
	if cq.Data != "restore_yes" {
		return
	}

	logger := r.logger.With(
		zap.Int64("tg_user_id", pending.tgUserID),
		zap.String("old_user_id", pending.oldUserID),
		zap.String("new_user_id", pending.newUserID),
	)
	result, err := repository.PerformRestore(ctx, r.db, pending.oldUserID, pending.newUserID, pending.tgUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			logger.Warn("telegram recovery bot: restore refused (not found or mismatch)")
			r.editMessage(cq.Message, "❌ Не удалось восстановить: привязка не найдена.")
			return
		}
		logger.Error("telegram recovery bot: PerformRestore failed", zap.Error(err))
		r.editMessage(cq.Message, "⚠️ Временная ошибка, попробуйте через минуту.")
		return
	}
	logger.Info("telegram recovery bot: restore complete",
		zap.Int64("devices_rebound", result.DevicesRebound),
		zap.Int64("sessions_deleted", result.SessionsDeleted),
	)
	// PERF-04 / D-06: PerformRestore rebinds devices from the new guest row to
	// the old (recovered) user and deletes sessions — both user ids change
	// state, so bust BOTH user:<id> entries. Without this, a recovered user
	// could be served a stale tier from cache, or the orphaned new-guest id
	// could linger. Best-effort — the 5s TTL is the backstop.
	if berr := cache.BustUserCache(context.Background(), r.rdb, result.OldUserID); berr != nil {
		logger.Warn("telegram recovery bot: BustUserCache(old) failed (5s TTL backstop)",
			zap.String("user_id", result.OldUserID), zap.Error(berr))
	}
	if berr := cache.BustUserCache(context.Background(), r.rdb, result.NewUserID); berr != nil {
		logger.Warn("telegram recovery bot: BustUserCache(new) failed (5s TTL backstop)",
			zap.String("user_id", result.NewUserID), zap.Error(berr))
	}
	r.writeAudit(ctx, "tg_restore", pending.oldUserID, pending.tgUserID, cq.Message.Chat.ID)
	r.editMessage(cq.Message,
		"✅ Подписка восстановлена.\n\n"+
			"Откройте приложение. Может потребоваться перезайти — нажмите "+
			"«Выйти» и «Войти как гость» в разделе «Аккаунт».",
	)
	r.notifyAdmin(pending, result)
}

// notifyAdmin DMs the support admin with a summary of a successful
// restore. Best-effort — failures are logged but do not affect the
// user-facing result. Skipped entirely when TelegramAdminChatID is
// unset in config.
func (r *Recovery) notifyAdmin(pending pendingRestore, result *repository.RestoreResult) {
	if r.cfg.TelegramAdminChatID == 0 {
		return
	}
	text := fmt.Sprintf(
		"🔁 tg_restore\n"+
			"old=<code>%s</code>\n"+
			"new=<code>%s</code>\n"+
			"tg_id=<code>%d</code>\n"+
			"devices_rebound=%d\n"+
			"sessions_deleted=%d",
		pending.oldUserID, pending.newUserID, pending.tgUserID,
		result.DevicesRebound, result.SessionsDeleted,
	)
	m := tgbotapi.NewMessage(r.cfg.TelegramAdminChatID, text)
	m.ParseMode = "HTML"
	if _, err := r.api.Send(m); err != nil {
		r.logger.Warn("telegram recovery bot: admin notification failed",
			zap.Int64("admin_chat_id", r.cfg.TelegramAdminChatID),
			zap.Error(err),
		)
	}
}

// writeAudit records the action in the audit_log table. Uses a
// synthetic admin_id (the subject of the action) because the
// audit_log schema requires a non-null admin_id with an FK to
// users — the bot is acting on behalf of the user here.
func (r *Recovery) writeAudit(ctx context.Context, action, targetUserID string, tgUserID int64, chatID int64) {
	details := model.AuditDetails{
		"source":      "telegram_recovery_bot",
		"tg_user_id":  tgUserID,
		"tg_chat_id":  chatID,
		"target_user": targetUserID,
	}
	target := targetUserID
	entry := &model.AuditLogEntry{
		AdminID:  targetUserID, // self-target: bot acts on behalf of the user
		Action:   action,
		TargetID: &target,
		Details:  details,
		IP:       "telegram",
	}
	if err := repository.CreateAuditEntry(ctx, r.db, entry); err != nil {
		r.logger.Warn("telegram recovery bot: audit write failed",
			zap.String("action", action),
			zap.Error(err),
		)
	}
}

// gcPendingRestores drops expired pending entries. Called after
// every update so the map never grows unbounded even under abuse.
func (r *Recovery) gcPendingRestores() {
	now := time.Now()
	for k, v := range r.pendingRestores {
		if now.After(v.expiresAt) {
			delete(r.pendingRestores, k)
		}
	}
}

// --- reply helpers --------------------------------------------------------

func (r *Recovery) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	if _, err := r.api.Send(msg); err != nil {
		r.logger.Warn("telegram recovery bot: send reply failed",
			zap.Int64("chat_id", chatID),
			zap.Error(err),
		)
	}
}

func (r *Recovery) editMessage(original *tgbotapi.Message, text string) {
	edit := tgbotapi.NewEditMessageText(original.Chat.ID, original.MessageID, text)
	edit.ParseMode = "HTML"
	if _, err := r.api.Send(edit); err != nil {
		r.logger.Warn("telegram recovery bot: edit message failed",
			zap.Int64("chat_id", original.Chat.ID),
			zap.Error(err),
		)
	}
}

func (r *Recovery) sendHelp(chatID int64) {
	text := "👋 Привет! Я бот восстановления аккаунта RiseVPN.\n\n" +
		"Я работаю только по ссылкам из приложения — откройте VPN, " +
		"раздел «Аккаунт» → «Привязать Telegram» или «Восстановить через Telegram»."
	r.reply(chatID, text)
}

func (r *Recovery) sendStatus(ctx context.Context, msg *tgbotapi.Message) {
	if msg.From == nil {
		return
	}
	user, err := repository.FindUserByTelegramID(ctx, r.db, msg.From.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			r.reply(msg.Chat.ID, "ℹ️ К этому Telegram не привязан ни один аккаунт VPN.")
			return
		}
		r.logger.Error("telegram recovery bot: status lookup failed", zap.Error(err))
		r.reply(msg.Chat.ID, "⚠️ Временная ошибка, попробуйте через минуту.")
		return
	}
	linkedAt := "—"
	if user.TelegramLinkedAt != nil {
		linkedAt = user.TelegramLinkedAt.Format("2006-01-02 15:04 UTC")
	}
	shortID := user.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	r.reply(msg.Chat.ID, fmt.Sprintf(
		"✅ Привязан VPN-аккаунт <code>%s…</code>\n"+
			"Тариф: %s\n"+
			"Привязан: %s",
		shortID, user.SubscriptionTier, linkedAt,
	))
}
