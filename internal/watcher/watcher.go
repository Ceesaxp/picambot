package watcher

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// EventType classifies a watch event by file type.
type EventType int

const (
	PhotoEvent EventType = iota
	VideoEvent
)

// WatchEvent is emitted for each new file of interest.
type WatchEvent struct {
	Type EventType
	Path string
	At   time.Time
}

// EventWatcher watches a directory and emits WatchEvents.
type EventWatcher interface {
	Watch(dir string, out chan<- WatchEvent) error
	Stop()
}

// FIFOWatcher reads file paths from a named pipe (FIFO) written by motion's
// on_movie_end / on_picture_save hooks. This guarantees the file is fully
// closed before we try to send it.
type FIFOWatcher struct {
	fifoPath string
	done     chan struct{}
	file     *os.File
	stopOnce sync.Once
}

// NewFIFOWatcher creates a FIFO at the given path (or reuses an existing one)
// and returns a watcher that reads completed file paths from it.
func NewFIFOWatcher(fifoPath string) (*FIFOWatcher, error) {
	// Create the FIFO if it doesn't already exist.
	if err := os.MkdirAll(filepath.Dir(fifoPath), 0o755); err != nil {
		return nil, fmt.Errorf("fifo parent dir: %w", err)
	}
	if _, err := os.Stat(fifoPath); os.IsNotExist(err) {
		if err := syscall.Mkfifo(fifoPath, 0o622); err != nil {
			return nil, fmt.Errorf("mkfifo %s: %w", fifoPath, err)
		}
	}
	return &FIFOWatcher{fifoPath: fifoPath, done: make(chan struct{})}, nil
}

// Watch starts reading from the FIFO and sending classified events to out.
// The dir parameter is unused (kept for interface compatibility).
func (fw *FIFOWatcher) Watch(_ string, out chan<- WatchEvent) error {
	// Keep both ends open to avoid EOF between writers. Nonblocking mode lets
	// Go's poller interrupt a pending read when Stop closes the descriptor.
	f, err := os.OpenFile(fw.fifoPath, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		f.Close()
		return fmt.Errorf("%s is not a FIFO", fw.fifoPath)
	}
	fw.file = f
	go func() {
		scanner := bufio.NewScanner(fifoReader{file: f, done: fw.done})
		for scanner.Scan() {
			line := scanner.Text()
			et, ok := classify(line)
			if !ok {
				continue
			}
			select {
			case <-fw.done:
				return
			default:
			}
			select {
			case out <- WatchEvent{Type: et, Path: line, At: time.Now()}:
			case <-fw.done:
				return
			}
		}
	}()
	return nil
}

// Stop terminates the watcher, including an idle FIFO read.
func (fw *FIFOWatcher) Stop() {
	fw.stopOnce.Do(func() {
		close(fw.done)
		if fw.file != nil {
			_ = fw.file.Close()
		}
	})
}

// FIFOPath returns the path to the FIFO, for use in motion config.
func (fw *FIFOWatcher) FIFOPath() string {
	return fw.fifoPath
}

func classify(path string) (EventType, bool) {
	switch filepath.Ext(path) {
	case ".jpg", ".jpeg":
		return PhotoEvent, true
	case ".mp4":
		return VideoEvent, true
	default:
		return 0, false
	}
}

// Some platforms do not register nonblocking FIFOs with Go's network poller.
// Retry EAGAIN there while retaining an interruptible, bounded wait.
type fifoReader struct {
	file *os.File
	done <-chan struct{}
}

func (r fifoReader) Read(p []byte) (int, error) {
	for {
		n, err := r.file.Read(p)
		if !errors.Is(err, syscall.EAGAIN) {
			return n, err
		}
		select {
		case <-r.done:
			return 0, io.EOF
		case <-time.After(25 * time.Millisecond):
		}
	}
}
