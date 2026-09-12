package notify

import (
	"context"
	"fmt"
	"sync/atomic"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Sender delivers messages, photos, and videos to a single Telegram chat.
type Sender struct {
	bot    *tgbotapi.BotAPI
	chatID atomic.Int64
}

// New creates a Sender targeting chatID.
func New(bot *tgbotapi.BotAPI, chatID int64) *Sender {
	s := &Sender{bot: bot}
	s.chatID.Store(chatID)
	return s
}

// SetChatID updates the target chat. Safe to call before the first send.
func (s *Sender) SetChatID(id int64) {
	s.chatID.Store(id)
}

// ChatID returns the current target chat ID.
func (s *Sender) ChatID() int64 {
	return s.chatID.Load()
}

// SendText sends a plain text message.
func (s *Sender) SendText(_ context.Context, text string) error {
	id := s.ChatID()
	if id == 0 {
		return fmt.Errorf("notify: no chat ID set")
	}
	msg := tgbotapi.NewMessage(id, text)
	_, err := s.bot.Send(msg)
	return err
}

// SendPhoto sends a JPEG file with an optional caption.
func (s *Sender) SendPhoto(_ context.Context, path, caption string) error {
	id := s.ChatID()
	if id == 0 {
		return fmt.Errorf("notify: no chat ID set")
	}
	photo := tgbotapi.NewPhoto(id, tgbotapi.FilePath(path))
	photo.Caption = caption
	_, err := s.bot.Send(photo)
	return err
}

// SendVideo sends an MP4 file with an optional caption.
func (s *Sender) SendVideo(_ context.Context, path, caption string) error {
	id := s.ChatID()
	if id == 0 {
		return fmt.Errorf("notify: no chat ID set")
	}
	video := tgbotapi.NewVideo(id, tgbotapi.FilePath(path))
	video.Caption = caption
	video.SupportsStreaming = true
	_, err := s.bot.Send(video)
	return err
}
