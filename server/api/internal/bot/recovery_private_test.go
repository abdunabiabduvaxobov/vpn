package bot

import (
	"testing"
)

// TestHandleUpdate_GroupChat_NoReply pins HARD-05 (D-13): the recovery bot must
// refuse non-private chats. A group/supergroup/channel message must produce NO
// reply — replying in a group leaks the deep-link/account-binding UX (and the
// chat id) to every member.
//
// The gate goes into handleUpdate (recovery.go:144), immediately after the
// `msg == nil || msg.From == nil` guard (recovery.go:158-160) and BEFORE
// msg.IsCommand():
//
//	if msg.Chat == nil || msg.Chat.Type != "private" {
//	    return // refuse group/supergroup/channel silently
//	}
//
// SKIP (compiling) now: the gate does not exist, and the bot's reply sink is a
// concrete *tgbotapi.BotAPI on the unexported Recovery struct — constructing a
// real Recovery requires live Telegram auth and the reply path is not yet
// injectable for a spy. When HARD-05 lands (and the reply sink is made
// fakeable), replace this skip with: feed handleUpdate an
// Update{Message:{Chat:{Type:"group"}, ...}} and assert the spy reply sink
// recorded zero sends. Flips GREEN when HARD-05 lands.
func TestHandleUpdate_GroupChat_NoReply(t *testing.T) {
	t.Skip("GREEN when HARD-05 private-chat gate lands in handleUpdate (group/supergroup/channel -> silent)")
}
