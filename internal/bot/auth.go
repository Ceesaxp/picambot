package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

// isAuthorised returns true if the message sender's ID is in the allowlist.
// Checks From.ID (sender), not Chat.ID, so it works correctly in both DMs and groups.
func isAuthorised(msg *tgbotapi.Message, allowedIDs []int64) bool {
	if msg == nil || msg.From == nil {
		return false
	}
	for _, id := range allowedIDs {
		if id == msg.From.ID {
			return true
		}
	}
	return false
}
