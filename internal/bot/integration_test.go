//go:build integration

package bot_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"picambot/internal/motion"
	"picambot/internal/state"
	"picambot/internal/watcher"
)

// fakeMotionClient records calls and returns canned paths.
type fakeMotionClient struct {
	mu             sync.Mutex
	startCalled    int
	stopCalled     int
	snapshotCalled int
	recordCalled   int
	detectStart    int
	detectPause    int
	snapshotPath   string
	recordPath     string
}

func (f *fakeMotionClient) Start(_ context.Context) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.startCalled++; return nil
}
func (f *fakeMotionClient) Stop(_ context.Context) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.stopCalled++; return nil
}
func (f *fakeMotionClient) Snapshot(_ context.Context) (string, error) {
	f.mu.Lock(); defer f.mu.Unlock(); f.snapshotCalled++; return f.snapshotPath, nil
}
func (f *fakeMotionClient) Record(_ context.Context, seconds int) (string, error) {
	if seconds < 1 || seconds > 300 {
		return "", motion.ErrOutOfRange
	}
	f.mu.Lock(); defer f.mu.Unlock(); f.recordCalled++; return f.recordPath, nil
}
func (f *fakeMotionClient) DetectionStart(_ context.Context) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.detectStart++; return nil
}
func (f *fakeMotionClient) DetectionPause(_ context.Context) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.detectPause++; return nil
}

// IT-01: /run → motion daemon starts; state transitions to monitoring.
func TestIntegration_Start(t *testing.T) {
	mc := &fakeMotionClient{}
	fsm := state.NewFSM(state.StateSleep)
	ctx := context.Background()

	if err := mc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := fsm.Transition(state.StateMonitoring); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if mc.startCalled != 1 {
		t.Errorf("startCalled = %d, want 1", mc.startCalled)
	}
	if fsm.Current() != state.StateMonitoring {
		t.Errorf("state = %s, want monitoring", fsm.Current())
	}
}

// IT-02: Snapshot → motion snapshot API called.
func TestIntegration_Snapshot(t *testing.T) {
	dir := t.TempDir()
	jpgPath := filepath.Join(dir, "snap.jpg")
	_ = os.WriteFile(jpgPath, []byte("jpeg"), 0o644)

	mc := &fakeMotionClient{snapshotPath: jpgPath}
	ctx := context.Background()

	path, err := mc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if mc.snapshotCalled != 1 {
		t.Errorf("snapshotCalled = %d, want 1", mc.snapshotCalled)
	}
	if path != jpgPath {
		t.Errorf("path = %q, want %q", path, jpgPath)
	}
}

// IT-03: /record 10 → motion record API called; returns an MP4 path.
func TestIntegration_Record(t *testing.T) {
	dir := t.TempDir()
	mp4Path := filepath.Join(dir, "clip.mp4")
	_ = os.WriteFile(mp4Path, []byte("mp4"), 0o644)

	mc := &fakeMotionClient{recordPath: mp4Path}
	ctx := context.Background()

	path, err := mc.Record(ctx, 10)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if mc.recordCalled != 1 {
		t.Errorf("recordCalled = %d, want 1", mc.recordCalled)
	}
	if path != mp4Path {
		t.Errorf("path = %q, want %q", path, mp4Path)
	}
}

// IT-04: /detect → detection enabled; state → detecting.
func TestIntegration_Detect(t *testing.T) {
	mc := &fakeMotionClient{}
	fsm := state.NewFSM(state.StateMonitoring)
	ctx := context.Background()

	if err := fsm.Transition(state.StateDetecting); err != nil {
		t.Fatalf("Transition to detecting: %v", err)
	}
	if err := mc.DetectionStart(ctx); err != nil {
		t.Fatalf("DetectionStart: %v", err)
	}
	if mc.detectStart != 1 {
		t.Errorf("detectStart = %d, want 1", mc.detectStart)
	}
	if fsm.Current() != state.StateDetecting {
		t.Errorf("state = %s, want detecting", fsm.Current())
	}
}

// IT-05: Path written to FIFO triggers watcher event.
func TestIntegration_MotionEvent(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")

	fw, err := watcher.NewFIFOWatcher(fifoPath)
	if err != nil {
		t.Fatalf("NewFIFOWatcher: %v", err)
	}

	out := make(chan watcher.WatchEvent, 1)
	if err := fw.Watch("", out); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(fw.Stop)

	jpgPath := filepath.Join(dir, "motion001.jpg")
	go func() {
		f, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		f.WriteString(jpgPath + "\n")
		f.Close()
	}()

	select {
	case ev := <-out:
		if ev.Type != watcher.PhotoEvent {
			t.Errorf("event type = %d, want PhotoEvent", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for motion event")
	}
}

// IT-06: /stop from detecting → detection paused; motion stopped; state → sleep.
func TestIntegration_StopFromDetecting(t *testing.T) {
	mc := &fakeMotionClient{}
	fsm := state.NewFSM(state.StateDetecting)
	ctx := context.Background()

	_ = mc.DetectionPause(ctx)
	if err := fsm.Transition(state.StateSleep); err != nil {
		t.Fatalf("Transition to sleep: %v", err)
	}
	_ = mc.Stop(ctx)

	if mc.detectPause != 1 {
		t.Errorf("detectPause = %d, want 1", mc.detectPause)
	}
	if mc.stopCalled != 1 {
		t.Errorf("stopCalled = %d, want 1", mc.stopCalled)
	}
	if fsm.Current() != state.StateSleep {
		t.Errorf("state = %s, want sleep", fsm.Current())
	}
}

// IT-07: Restart recovery — bot restarts with detecting in state.json; re-arms detection.
func TestIntegration_RestartRecovery(t *testing.T) {
	dir := t.TempDir()
	store := state.NewPersistentStateStore(filepath.Join(dir, "state.json"))
	if err := store.Save(state.StateDetecting, 42); err != nil {
		t.Fatal(err)
	}

	// Simulate a restart: load persisted state.
	st, chatID, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st != state.StateDetecting {
		t.Errorf("state = %s, want detecting", st)
	}
	if chatID != 42 {
		t.Errorf("chatID = %d, want 42", chatID)
	}

	mc := &fakeMotionClient{}
	fsm := state.NewFSM(st)

	// Simulate Run() startup crash-recovery path.
	if fsm.Current() == state.StateDetecting {
		if err := mc.DetectionStart(context.Background()); err != nil {
			t.Fatalf("DetectionStart on recovery: %v", err)
		}
	}

	if mc.detectStart != 1 {
		t.Errorf("detectStart = %d, want 1 after restart recovery", mc.detectStart)
	}
}
