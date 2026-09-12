package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"picambot/internal/notify"
	"picambot/internal/state"
)

type fakeMotionClient struct {
	start, stop, detect, pause             int
	startErr, stopErr, detectErr, pauseErr error
}

func (f *fakeMotionClient) Start(context.Context) error               { f.start++; return f.startErr }
func (f *fakeMotionClient) Stop(context.Context) error                { f.stop++; return f.stopErr }
func (f *fakeMotionClient) DetectionStart(context.Context) error      { f.detect++; return f.detectErr }
func (f *fakeMotionClient) DetectionPause(context.Context) error      { f.pause++; return f.pauseErr }
func (*fakeMotionClient) Snapshot(context.Context) (string, error)    { return "", nil }
func (*fakeMotionClient) Record(context.Context, int) (string, error) { return "", nil }

func newControlBot(t *testing.T, initial state.State) (*Bot, *fakeMotionClient) {
	t.Helper()
	dir := t.TempDir()
	mc := &fakeMotionClient{}
	b := New(nil, BotConfig{AllowedUserIDs: []int64{42}}, state.NewFSM(initial),
		state.NewPersistentStateStore(filepath.Join(dir, "state.json")), mc,
		notify.New(nil, 0), nil, dir, filepath.Join(dir, "events.fifo"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(b.stopWatcher)
	return b, mc
}

func TestMonitoringSwitchCycle(t *testing.T) {
	b, mc := newControlBot(t, state.StateSleep)
	b.sender.SetChatID(123)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := b.SetMonitoring(ctx, true); err != nil {
			t.Fatal(err)
		}
	}
	if !b.Monitoring() || mc.start != 1 || mc.detect != 1 {
		t.Fatalf("on: state=%s start=%d detect=%d", b.fsm.Current(), mc.start, mc.detect)
	}
	st, chat, err := b.store.Load()
	if err != nil || st != state.StateDetecting || chat != 123 {
		t.Fatalf("persisted: %s %d %v", st, chat, err)
	}
	for i := 0; i < 2; i++ {
		if err := b.SetMonitoring(ctx, false); err != nil {
			t.Fatal(err)
		}
	}
	if b.Monitoring() || mc.stop != 1 || b.activeWatcher != nil {
		t.Fatal("off did not stop camera and watcher")
	}
	st, chat, err = b.store.Load()
	if err != nil || st != state.StateSleep || chat != 123 {
		t.Fatalf("persisted: %s %d %v", st, chat, err)
	}
}

func TestTelegramAndHomeShareState(t *testing.T) {
	b, mc := newControlBot(t, state.StateSleep)
	ctx := context.Background()
	dispatch := func(command string) {
		b.dispatch(ctx, &tgbotapi.Message{From: &tgbotapi.User{ID: 42}, Chat: &tgbotapi.Chat{ID: 0}, Text: command,
			Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: len(command)}}})
	}
	dispatch("/run")
	if err := b.SetMonitoring(ctx, true); err != nil {
		t.Fatal(err)
	}
	if mc.start != 1 {
		t.Fatal("Home restarted an already running camera")
	}
	dispatch("/stopdetect")
	if b.Monitoring() || b.fsm.Current() != state.StateMonitoring {
		t.Fatal("Telegram pause not reflected")
	}
	if err := b.SetMonitoring(ctx, false); err != nil {
		t.Fatal(err)
	}
	if b.fsm.Current() != state.StateSleep {
		t.Fatal("off must stop even when detection already off")
	}
	dispatch("/run")
	dispatch("/detect")
	if !b.Monitoring() {
		t.Fatal("Telegram detection not reflected")
	}
	select {
	case <-b.MonitoringChanges():
	default:
		t.Fatal("missing status notification")
	}
	dispatch("/stop")
	if b.Monitoring() {
		t.Fatal("Telegram stop not reflected")
	}
}

func TestMonitoringFailuresRemainRetryable(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("hardware unavailable")
	t.Run("start", func(t *testing.T) {
		b, mc := newControlBot(t, state.StateSleep)
		mc.startErr = boom
		if !errors.Is(b.SetMonitoring(ctx, true), boom) || b.Monitoring() || mc.detect != 0 {
			t.Fatal("start failure accepted")
		}
	})
	t.Run("detect", func(t *testing.T) {
		b, mc := newControlBot(t, state.StateSleep)
		mc.detectErr = boom
		if !errors.Is(b.SetMonitoring(ctx, true), boom) || b.Monitoring() || b.activeWatcher != nil {
			t.Fatal("detect failure accepted")
		}
		mc.detectErr = nil
		if err := b.SetMonitoring(ctx, true); err != nil {
			t.Fatal(err)
		}
		if mc.start != 1 {
			t.Fatal("retry restarted camera")
		}
	})
	t.Run("stop", func(t *testing.T) {
		b, mc := newControlBot(t, state.StateSleep)
		if err := b.SetMonitoring(ctx, true); err != nil {
			t.Fatal(err)
		}
		mc.stopErr = boom
		if !errors.Is(b.SetMonitoring(ctx, false), boom) || !b.Monitoring() || b.activeWatcher == nil {
			t.Fatal("failed stop claimed sleep")
		}
		mc.stopErr = nil
		if err := b.SetMonitoring(ctx, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("watcher", func(t *testing.T) {
		b, mc := newControlBot(t, state.StateMonitoring)
				// A regular parent file cannot contain a FIFO.
		b.fifoPath = filepath.Join(b.targetDir, "state.json", "events.fifo")
		if err := b.persistState(); err != nil {
			t.Fatal(err)
		}
		if b.SetMonitoring(ctx, true) == nil || mc.detect != 0 {
			t.Fatal("enabled detection without watcher")
		}
	})
	t.Run("recording", func(t *testing.T) {
		b, mc := newControlBot(t, state.StateRecording)
		if b.SetMonitoring(ctx, true) == nil || mc.detect != 0 {
			t.Fatal("detection interrupted recording")
		}
		cancelled := false
		b.recordCancel = func() { cancelled = true }
		if err := b.SetMonitoring(ctx, false); err != nil {
			t.Fatal(err)
		}
		if !cancelled || b.fsm.Current() != state.StateSleep {
			t.Fatal("off did not cancel recording")
		}
	})
}

func TestConcurrentMonitoringCommands(t *testing.T) {
	b, _ := newControlBot(t, state.StateSleep)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(on bool) {
			defer wg.Done()
			if err := b.SetMonitoring(context.Background(), on); err != nil {
				t.Error(err)
			}
		}(i%2 == 0)
	}
	wg.Wait()
	if err := b.SetMonitoring(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st, _, err := b.store.Load()
	if err != nil || st != state.StateSleep {
		t.Fatalf("state: %s %v", st, err)
	}
}
