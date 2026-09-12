package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"picambot/internal/homekit"
	"strconv"
	"strings"
)

// Config holds all runtime configuration sourced from environment variables.
type Config struct {
	HomeKit         homekit.Config
	TelegramToken   string
	AllowedUserIDs  []int64
	MotionHost      string
	MotionPort      int
	MotionTargetDir string
	StateFile       string
	FFmpegPath      string
	LogLevel        slog.Level
}

// LoadConfig reads environment variables, validates required ones, and applies defaults.
// Returns an error listing all missing required variables.
func LoadConfig() (Config, error) {
	var missing []string

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		missing = append(missing, "TELEGRAM_TOKEN")
	}

	rawIDs := os.Getenv("ALLOWED_USER_IDS")
	if rawIDs == "" {
		missing = append(missing, "ALLOWED_USER_IDS")
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	ids, err := parseUserIDs(rawIDs)
	if err != nil {
		return Config{}, fmt.Errorf("invalid ALLOWED_USER_IDS: %w", err)
	}

	host := os.Getenv("MOTION_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	port := 7999
	if raw := os.Getenv("MOTION_PORT"); raw != "" {
		port, err = strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MOTION_PORT: %w", err)
		}
	}

	targetDir := os.Getenv("MOTION_TARGET_DIR")
	if targetDir == "" {
		targetDir = "/var/lib/motion"
	}

	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		stateFile = "/var/lib/picambot/state.json"
	}

	ffmpegPath := os.Getenv("FFMPEG_PATH")
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	logLevel := slog.LevelInfo
	if raw := os.Getenv("LOG_LEVEL"); raw != "" {
		if err := logLevel.UnmarshalText([]byte(raw)); err != nil {
			return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: %w", raw, err)
		}
	}

	hk := homekit.Config{
		Name:       "Camera monitoring",
		PIN:        os.Getenv("HOMEKIT_PIN"),
		StorageDir: filepath.Join(filepath.Dir(stateFile), "homekit"),
		Address:    ":51826",
	}
	if raw := os.Getenv("HOMEKIT_ENABLED"); raw != "" {
		hk.Enabled, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid HOMEKIT_ENABLED: %w", err)
		}
	}
	if v := os.Getenv("HOMEKIT_NAME"); v != "" {
		hk.Name = v
	}
	if v := os.Getenv("HOMEKIT_STORAGE_DIR"); v != "" {
		hk.StorageDir = v
	}
	if err := hk.Validate(); err != nil {
		return Config{}, err
	}

	return Config{
		HomeKit:         hk,
		TelegramToken:   token,
		AllowedUserIDs:  ids,
		MotionHost:      host,
		MotionPort:      port,
		MotionTargetDir: targetDir,
		StateFile:       stateFile,
		FFmpegPath:      ffmpegPath,
		LogLevel:        logLevel,
	}, nil
}

func parseUserIDs(raw string) ([]int64, error) {
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	var errs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%q is not a valid int64", p))
			continue
		}
		ids = append(ids, id)
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	if len(ids) == 0 {
		return nil, errors.New("no valid user IDs provided")
	}
	return ids, nil
}
