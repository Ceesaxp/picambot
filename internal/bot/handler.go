package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"picambot/internal/motion"
	"picambot/internal/state"
)

func (b *Bot) handleStart(ctx context.Context, msg *tgbotapi.Message) {
	if msg.Chat.ID != 0 {
		b.sender.SetChatID(msg.Chat.ID)
	}
	if err := b.startCamera(ctx); err != nil {
		b.logger.Error("camera start failed", "err", err)
		b.reply(ctx, "❌ Cannot start camera: "+err.Error())
		return
	}
	b.reply(ctx, "✅ Camera online. Ready for commands.")
}

func (b *Bot) handleStop(ctx context.Context, msg *tgbotapi.Message) {
	if msg.Chat.ID != 0 {
		b.sender.SetChatID(msg.Chat.ID)
	}
	if err := b.stopCamera(ctx); err != nil {
		b.logger.Error("camera stop failed", "err", err)
		b.reply(ctx, "❌ Cannot stop camera: "+err.Error())
		return
	}
	b.reply(ctx, "✅ Camera offline. System sleeping.")
}

func (b *Bot) handleSnapshot(ctx context.Context, msg *tgbotapi.Message) {
	if b.fsm.Current() == state.StateSleep {
		b.reply(ctx, "Camera is sleeping. Send /run first.")
		return
	}
	path, err := b.motion.Snapshot(ctx)
	if err != nil {
		b.logger.Error("snapshot failed", "err", err)
		b.reply(ctx, "❌ Snapshot failed.")
		return
	}
	caption := fmt.Sprintf("📷 Snapshot — %s", time.Now().Format("15:04:05"))
	if err := b.sender.SendPhoto(ctx, path, caption); err != nil {
		b.logger.Error("send photo failed", "err", err)
	}
}

func (b *Bot) handleRecord(ctx context.Context, msg *tgbotapi.Message) {
	if b.fsm.Current() == state.StateSleep {
		b.reply(ctx, "Camera is sleeping. Send /run first.")
		return
	}

	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		b.reply(ctx, "Usage: /record <seconds> (1–300)")
		return
	}
	seconds, err := strconv.Atoi(args)
	if err != nil {
		b.reply(ctx, "Usage: /record <seconds> (1–300)")
		return
	}

	if err := b.fsm.Transition(state.StateRecording); err != nil {
		b.reply(ctx, "Cannot record right now.")
		return
	}
	b.saveState(msg.Chat.ID)

	recordCtx, cancel := context.WithCancel(ctx)
	b.recordMu.Lock()
	b.recordCancel = cancel
	done := make(chan struct{})
	b.recordDone = done
	b.recordMu.Unlock()

	go func() {
		defer cancel()
		path, err := b.motion.Record(recordCtx, seconds)
		close(done)
		b.controlMu.Lock()
		if recordCtx.Err() != nil {
			b.controlMu.Unlock()
			return
		}
		b.recordMu.Lock()
		b.recordCancel = nil
		b.recordMu.Unlock()
		b.fsm.Set(state.StateMonitoring)
		b.saveState(msg.Chat.ID)
		b.controlMu.Unlock()
		if err != nil {
			if err == motion.ErrOutOfRange {
				b.reply(ctx, "❌ Duration must be 1–300 seconds.")
			} else {
				b.logger.Error("record failed", "err", err)
				b.reply(ctx, "❌ Recording failed.")
			}
			return
		}

		muxed, muxErr := b.muxer.Mux(ctx, path)
		if muxErr != nil {
			b.logger.Error("audio mux failed; sending video without audio", "err", muxErr)
			muxed = path
		}

		caption := fmt.Sprintf("🎥 Recording — %ds — %s", seconds, time.Now().Format("15:04:05"))
		if err := b.sender.SendVideo(ctx, muxed, caption); err != nil {
			b.logger.Error("send video failed", "err", err)
		}
		if muxed != path {
			if err := os.Remove(muxed); err != nil && !os.IsNotExist(err) {
				b.logger.Warn("remove muxed clip", "path", muxed, "err", err)
			}
		}
	}()
}

func (b *Bot) handleDetect(ctx context.Context, msg *tgbotapi.Message) {
	if msg.Chat.ID != 0 {
		b.sender.SetChatID(msg.Chat.ID)
	}
	if err := b.enableDetection(ctx); err != nil {
		b.logger.Error("detection start failed", "err", err)
		b.reply(ctx, "❌ Cannot enable detection: "+err.Error())
		return
	}
	b.reply(ctx, "✅ Motion detection enabled.")
}

func (b *Bot) handleStopDetect(ctx context.Context, msg *tgbotapi.Message) {
	if msg.Chat.ID != 0 {
		b.sender.SetChatID(msg.Chat.ID)
	}
	if err := b.disableDetection(ctx); err != nil {
		b.logger.Error("detection pause failed", "err", err)
		b.reply(ctx, "❌ Cannot disable detection: "+err.Error())
		return
	}
	b.reply(ctx, "✅ Motion detection disabled.")
}

func (b *Bot) handleStatus(_ context.Context, _ *tgbotapi.Message) {
	cur := b.fsm.Current()
	var icon string
	switch cur {
	case state.StateSleep:
		icon = "🔴"
	case state.StateMonitoring:
		icon = "🟢"
	case state.StateRecording:
		icon = "🔵"
	case state.StateDetecting:
		icon = "🟡"
	}

	uptime := time.Since(b.startedAt).Round(time.Second)
	text := fmt.Sprintf("%s State: %s\n⏱ Uptime: %s", icon, cur, formatDuration(uptime))
	b.reply(context.Background(), text)
}

func (b *Bot) handleHelp(_ context.Context, _ *tgbotapi.Message) {
	text := `/run — Wake the camera system
/stop — Return to sleep
/snapshot — Capture a still image
/record <N> — Record N seconds (1–300)
/detect — Enable motion detection
/stopdetect — Disable motion detection
/status — Show current state
/help — This message`
	b.reply(context.Background(), text)
}

func (b *Bot) reply(ctx context.Context, text string) {
	if err := b.sender.SendText(ctx, text); err != nil {
		b.logger.Error("reply failed", "err", err)
	}
}

func (b *Bot) saveState(chatID int64) {
	if chatID != 0 {
		b.sender.SetChatID(chatID)
	}
	if err := b.persistState(); err != nil {
		b.logger.Error("state save failed", "err", err)
	}
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
