package main

import (
	"strings"
	"testing"
)

func TestLoadConfig_MissingRequired(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "both missing",
			env:     map[string]string{},
			wantErr: "TELEGRAM_TOKEN, ALLOWED_USER_IDS",
		},
		{
			name:    "token missing",
			env:     map[string]string{"ALLOWED_USER_IDS": "123"},
			wantErr: "TELEGRAM_TOKEN",
		},
		{
			name:    "user IDs missing",
			env:     map[string]string{"TELEGRAM_TOKEN": "tok"},
			wantErr: "ALLOWED_USER_IDS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := LoadConfig()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("TELEGRAM_TOKEN", "mytoken")
	t.Setenv("ALLOWED_USER_IDS", "42,99")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.TelegramToken != "mytoken" {
		t.Errorf("TelegramToken = %q", cfg.TelegramToken)
	}
	if len(cfg.AllowedUserIDs) != 2 || cfg.AllowedUserIDs[0] != 42 || cfg.AllowedUserIDs[1] != 99 {
		t.Errorf("AllowedUserIDs = %v", cfg.AllowedUserIDs)
	}
	if cfg.MotionPort != 7999 {
		t.Errorf("MotionPort = %d", cfg.MotionPort)
	}
	if cfg.MotionTargetDir != "/var/lib/motion" {
		t.Errorf("MotionTargetDir = %q", cfg.MotionTargetDir)
	}
	if cfg.StateFile != "/var/lib/picambot/state.json" {
		t.Errorf("StateFile = %q", cfg.StateFile)
	}
}

func TestLoadConfig_InvalidUserIDs(t *testing.T) {
	clearEnv(t)
	t.Setenv("TELEGRAM_TOKEN", "tok")
	t.Setenv("ALLOWED_USER_IDS", "notanumber")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ALLOWED_USER_IDS") {
		t.Errorf("error should mention ALLOWED_USER_IDS, got: %v", err)
	}
}

// clearEnv unsets all picambot env vars so tests start clean.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"TELEGRAM_TOKEN", "ALLOWED_USER_IDS", "MOTION_HOST",
		"MOTION_PORT", "MOTION_TARGET_DIR", "STATE_FILE", "LOG_LEVEL", "FFMPEG_PATH",
		"HOMEKIT_ENABLED", "HOMEKIT_PIN", "HOMEKIT_NAME", "HOMEKIT_STORAGE_DIR",
	} {
		t.Setenv(k, "")
	}
}

func TestHomeKitConfig(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, pin string
		wantErr            bool
	}{
		{"disabled", "", "", false},
		{"enabled", "true", "24681357", false},
		{"missing pin", "true", "", true},
		{"bad pin", "true", "abcdefgh", true},
		{"insecure pin", "true", "12345678", true},
		{"invalid boolean", "maybe", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("TELEGRAM_TOKEN", "tok")
			t.Setenv("ALLOWED_USER_IDS", "42")
			t.Setenv("HOMEKIT_ENABLED", tc.enabled)
			t.Setenv("HOMEKIT_PIN", tc.pin)
			cfg, err := LoadConfig()
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if err == nil && cfg.HomeKit.Enabled != (tc.enabled == "true") {
				t.Fatal("wrong enabled state")
			}
		})
	}
}
