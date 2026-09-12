package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"picambot/internal/audio"
	"picambot/internal/bot"
	"picambot/internal/homekit"
	"picambot/internal/motion"
	"picambot/internal/notify"
	"picambot/internal/state"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "picambot: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	logger.Info("bot authorised", "username", api.Self.UserName, "version", version)

	store := state.NewPersistentStateStore(cfg.StateFile)
	st, chatID, err := store.Load()
	if err != nil {
		return fmt.Errorf("state load: %w", err)
	}
	logger.Info("state loaded", "state", st, "chat_id", chatID)

	fsm := state.NewFSM(st)

	mc := motion.NewHTTPMotionClient(cfg.MotionHost, cfg.MotionPort, cfg.MotionTargetDir)

	sender := notify.New(api, chatID)

	muxer := audio.New(cfg.FFmpegPath)

	// FIFO lives next to the state file — motion hooks write completed file paths here.
	fifoPath := filepath.Join(filepath.Dir(cfg.StateFile), "events.fifo")

	b := bot.New(
		api,
		bot.BotConfig{AllowedUserIDs: cfg.AllowedUserIDs},
		fsm,
		store,
		mc,
		sender,
		muxer,
		cfg.MotionTargetDir,
		fifoPath,
		logger,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := b.Recover(ctx); err != nil {
		logger.Error("camera recovery failed", "err", err)
	}
	if !cfg.HomeKit.Enabled {
		logger.Info("starting bot")
		return b.Run(ctx)
	}
	hk, err := homekit.New(ctx, cfg.HomeKit, b, logger)
	if err != nil {
		return fmt.Errorf("homekit: %w", err)
	}
	logger.Info("starting bot with HomeKit switch", "name", cfg.HomeKit.Name, "address", cfg.HomeKit.Address)
	results := make(chan error, 2)
	go func() { results <- b.Run(ctx) }()
	go func() { results <- hk.Run(ctx) }()
	err = <-results
	stop()
	<-results
	return err
}
