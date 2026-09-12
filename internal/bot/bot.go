package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"picambot/internal/audio"
	"picambot/internal/motion"
	"picambot/internal/notify"
	"picambot/internal/state"
	"picambot/internal/watcher"
)

// Bot is the central coordinator: it polls Telegram, dispatches commands,
// and routes motion watch events to Telegram notifications.
type Bot struct {
	controlMu         sync.Mutex
	monitoringChanges chan struct{}
	api               *tgbotapi.BotAPI
	cfg               BotConfig
	fsm               *state.FSM
	store             state.StateStore
	motion            motion.MotionClient
	sender            *notify.Sender
	muxer             *audio.Muxer
	logger            *slog.Logger
	targetDir         string
	fifoPath          string
	startedAt         time.Time

	// in-flight /record cancellation
	recordDone   chan struct{}
	recordCancel context.CancelFunc
	recordMu     sync.Mutex

	// active EventWatcher (nil when not detecting)
	activeWatcher watcher.EventWatcher
	watcherMu     sync.Mutex
	watchEvents   chan watcher.WatchEvent
}

// BotConfig holds the subset of Config the Bot needs.
type BotConfig struct {
	AllowedUserIDs []int64
}

// New wires up a Bot with all its dependencies.
func New(
	api *tgbotapi.BotAPI,
	cfg BotConfig,
	fsm *state.FSM,
	store state.StateStore,
	mc motion.MotionClient,
	sender *notify.Sender,
	muxer *audio.Muxer,
	targetDir string,
	fifoPath string,
	logger *slog.Logger,
) *Bot {
	return &Bot{
		monitoringChanges: make(chan struct{}, 1),
		api:               api,
		cfg:               cfg,
		fsm:               fsm,
		store:             store,
		motion:            mc,
		sender:            sender,
		muxer:             muxer,
		targetDir:         targetDir,
		fifoPath:          fifoPath,
		logger:            logger,
		watchEvents:       make(chan watcher.WatchEvent, 16),
		startedAt:         time.Now(),
	}
}

// Run starts the Telegram polling loop. It blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	defer b.stopWatcher()
	defer b.api.StopReceivingUpdates()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev, ok := <-b.watchEvents:
			if !ok {
				continue
			}
			b.handleWatchEvent(ctx, ev)

		case update, ok := <-updates:
			if !ok {
				return fmt.Errorf("telegram update channel closed")
			}
			if update.Message == nil {
				continue
			}
			b.dispatch(ctx, update.Message)
		}
	}
}

func (b *Bot) dispatch(ctx context.Context, msg *tgbotapi.Message) {
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	if !isAuthorised(msg, b.cfg.AllowedUserIDs) {
		b.logger.Warn("unauthorized message", "from_id", msg.From.ID, "chat_id", msg.Chat.ID)
		return
	}

	// Keep sender pointed at the authorised user's chat.
	if b.sender.ChatID() == 0 {
		b.sender.SetChatID(msg.Chat.ID)
	}

	switch msg.Command() {
	case "run":
		b.handleStart(ctx, msg)
	case "stop":
		b.handleStop(ctx, msg)
	case "snapshot":
		b.handleSnapshot(ctx, msg)
	case "record":
		b.handleRecord(ctx, msg)
	case "detect":
		b.handleDetect(ctx, msg)
	case "stopdetect":
		b.handleStopDetect(ctx, msg)
	case "status":
		b.handleStatus(ctx, msg)
	case "help", "":
		b.handleHelp(ctx, msg)
	default:
		b.reply(ctx, "Unknown command. Send /help for a list of commands.")
	}
}

func (b *Bot) handleWatchEvent(ctx context.Context, ev watcher.WatchEvent) {
	ts := ev.At.Format("15:04:05")
	switch ev.Type {
	case watcher.PhotoEvent:
		caption := fmt.Sprintf("🚨 Motion detected — %s", ts)
		if err := b.sender.SendPhoto(ctx, ev.Path, caption); err != nil {
			b.logger.Error("send motion photo failed", "err", err)
		}
	case watcher.VideoEvent:
		path, err := b.muxer.Mux(ctx, ev.Path)
		if err != nil {
			b.logger.Error("audio mux failed; sending video without audio", "err", err)
			path = ev.Path
		}
		caption := fmt.Sprintf("🎥 Motion clip saved — %s", ts)
		if err := b.sender.SendVideo(ctx, path, caption); err != nil {
			b.logger.Error("send motion video failed", "err", err)
		}
		if path != ev.Path {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				b.logger.Warn("remove muxed clip", "path", path, "err", err)
			}
		}
	}
}

func (b *Bot) startWatcher(dir string) error {
	b.watcherMu.Lock()
	defer b.watcherMu.Unlock()

	if b.activeWatcher != nil {
		return nil // already watching
	}

	fw, err := watcher.NewFIFOWatcher(b.fifoPath)
	if err != nil {
		return err
	}
	if err := fw.Watch(dir, b.watchEvents); err != nil {
		fw.Stop()
		return err
	}
	b.activeWatcher = fw
	return nil
}

func (b *Bot) stopWatcher() {
	b.watcherMu.Lock()
	defer b.watcherMu.Unlock()

	if b.activeWatcher != nil {
		b.activeWatcher.Stop()
		b.activeWatcher = nil
	}
}
