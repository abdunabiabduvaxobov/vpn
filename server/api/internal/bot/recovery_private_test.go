package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// recordingBot returns a *Recovery whose Telegram API endpoint points at an
// httptest server that counts every outbound Bot API request (a "send"). The
// reply sink (r.api) is a real *tgbotapi.BotAPI, but every request is captured
// instead of hitting Telegram — so we can assert how many times the bot replied.
//
// This is the injectable spy the original Wave-0 skip said did not exist: the
// test lives in `package bot`, so it can set the unexported `api` field
// directly and aim it at a local recorder via SetAPIEndpoint.
func recordingBot(t *testing.T) (*Recovery, *int32) {
	t.Helper()
	var sends int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&sends, 1)
		// Minimal well-formed Bot API response so tgbotapi.Send does not error.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"},"text":"x"}}`))
	}))
	t.Cleanup(srv.Close)

	api := &tgbotapi.BotAPI{Token: "test:token", Client: srv.Client(), Buffer: 100}
	api.SetAPIEndpoint(srv.URL + "/bot%s/%s")

	return &Recovery{api: api, logger: zap.NewNop()}, &sends
}

// helpCommand builds a "/help" message Update for the given chat type, with the
// bot_command entity set so msg.IsCommand() / msg.Command() resolve correctly.
func helpCommand(chatType string, chatID int64) tgbotapi.Update {
	return tgbotapi.Update{Message: &tgbotapi.Message{
		From:     &tgbotapi.User{ID: 4242},
		Chat:     &tgbotapi.Chat{ID: chatID, Type: chatType},
		Text:     "/help",
		Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: 5}},
	}}
}

// TestHandleUpdate_GroupChat_NoReply pins HARD-05 (D-13 / audit S1-8): the
// recovery bot must refuse non-private chats. A group/supergroup/channel message
// must produce NO reply — replying in a group leaks the deep-link/account-binding
// UX and the chat id to every member. The gate lives at recovery.go:165
// (`msg.Chat == nil || msg.Chat.Type != "private"` -> return) ahead of dispatch.
func TestHandleUpdate_GroupChat_NoReply(t *testing.T) {
	for _, chatType := range []string{"group", "supergroup", "channel"} {
		t.Run(chatType+" command produces zero replies", func(t *testing.T) {
			r, sends := recordingBot(t)
			r.handleUpdate(context.Background(), helpCommand(chatType, -1000))
			if got := atomic.LoadInt32(sends); got != 0 {
				t.Errorf("HARD-05: a %s-chat /help triggered %d send(s) — the bot must stay silent outside private chats", chatType, got)
			}
		})
	}
}

// TestHandleUpdate_PrivateChat_Replies is the positive control: the same /help
// command in a PRIVATE chat MUST reach the reply sink. Without it, the group
// test above could pass simply because nothing ever sends — this proves the gate
// (not a dead handler) is what suppresses the group reply.
func TestHandleUpdate_PrivateChat_Replies(t *testing.T) {
	r, sends := recordingBot(t)
	r.handleUpdate(context.Background(), helpCommand("private", 555))
	if got := atomic.LoadInt32(sends); got < 1 {
		t.Errorf("HARD-05 control: a private /help triggered %d sends, want >=1 — the dispatch path is broken, so the group assertion is meaningless", got)
	}
}
