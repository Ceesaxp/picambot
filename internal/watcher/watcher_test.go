package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// UT-08: Photo path written to FIFO emits PhotoEvent.
// UT-09: Video path written to FIFO emits VideoEvent.
func TestFIFOWatcher_Events(t *testing.T) {
	tests := []struct {
		filename string
		wantType EventType
	}{
		{"motion.jpg", PhotoEvent},
		{"motion.jpeg", PhotoEvent},
		{"clip.mp4", VideoEvent},
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			dir := t.TempDir()
			fifoPath := filepath.Join(dir, "events.fifo")

			fw, err := NewFIFOWatcher(fifoPath)
			if err != nil {
				t.Fatalf("NewFIFOWatcher: %v", err)
			}

			out := make(chan WatchEvent, 1)
			if err := fw.Watch("", out); err != nil {
				t.Fatalf("Watch: %v", err)
			}
			t.Cleanup(fw.Stop)

			// Write a path to the FIFO (simulating motion's hook).
			fakePath := filepath.Join("/var/lib/motion", tc.filename)
			go func() {
				f, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
				if err != nil {
					return
				}
				f.WriteString(fakePath + "\n")
				f.Close()
			}()

			select {
			case ev := <-out:
				if ev.Type != tc.wantType {
					t.Errorf("EventType = %d, want %d", ev.Type, tc.wantType)
				}
				if ev.Path != fakePath {
					t.Errorf("Path = %q, want %q", ev.Path, fakePath)
				}
			case <-time.After(3 * time.Second):
				t.Error("timed out waiting for watch event")
			}
		})
	}
}

func TestFIFOWatcher_IgnoresUnknownExtensions(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "events.fifo")

	fw, err := NewFIFOWatcher(fifoPath)
	if err != nil {
		t.Fatalf("NewFIFOWatcher: %v", err)
	}

	out := make(chan WatchEvent, 1)
	if err := fw.Watch("", out); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(fw.Stop)

	go func() {
		f, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		f.WriteString("/var/lib/motion/ignored.txt\n")
		f.Close()
	}()

	select {
	case ev := <-out:
		t.Errorf("unexpected event for ignored file: %+v", ev)
	case <-time.After(500 * time.Millisecond):
		// correct — nothing emitted
	}
}

func TestFIFOWatcher_StopAndRestart(t *testing.T) {
	fifoPath := filepath.Join(t.TempDir(), "events.fifo")
	for i := 0; i < 5; i++ {
		fw, err := NewFIFOWatcher(fifoPath)
		if err != nil {
			t.Fatal(err)
		}
		out := make(chan WatchEvent, 1)
		if err := fw.Watch("", out); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString("motion.jpg\n")
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-out:
		case <-time.After(time.Second):
			t.Fatal("restarted watcher lost event")
		}
		fw.Stop()
		fw.Stop()
		if _, err := fw.file.Stat(); err == nil {
			t.Fatal("stopped watcher retained descriptor")
		}
	}
}
