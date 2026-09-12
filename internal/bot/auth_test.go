package bot

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// UT-06: Message from non-whitelisted From.ID is not authorised.
// UT-07: Message from whitelisted From.ID is authorised (DM or group).
func TestIsAuthorised(t *testing.T) {
	allowed := []int64{111, 222}

	tests := []struct {
		name string
		msg  *tgbotapi.Message
		want bool
	}{
		{
			name: "nil message",
			msg:  nil,
			want: false,
		},
		{
			name: "nil From",
			msg:  &tgbotapi.Message{},
			want: false,
		},
		{
			name: "non-whitelisted user",
			msg:  &tgbotapi.Message{From: &tgbotapi.User{ID: 999}},
			want: false,
		},
		{
			name: "first whitelisted user (DM context)",
			msg:  &tgbotapi.Message{From: &tgbotapi.User{ID: 111}},
			want: true,
		},
		{
			name: "second whitelisted user (group context)",
			msg: &tgbotapi.Message{
				From: &tgbotapi.User{ID: 222},
				Chat: &tgbotapi.Chat{ID: -1001234567890, Type: "supergroup"},
			},
			want: true,
		},
		{
			name: "whitelisted chat ID but wrong From.ID",
			msg: &tgbotapi.Message{
				From: &tgbotapi.User{ID: 333},
				Chat: &tgbotapi.Chat{ID: 111},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAuthorised(tc.msg, allowed)
			if got != tc.want {
				t.Errorf("isAuthorised = %v, want %v", got, tc.want)
			}
		})
	}
}
