// Package audio combines motion-produced MP4 clips with sibling WAV files
// recorded by arecord (driven from motion's on_event_start/on_event_end hooks).
package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Muxer pairs an MP4 with a sibling WAV (same basename, .wav extension) and
// produces a new MP4 carrying both streams. When the WAV is absent the
// original MP4 path is returned unchanged, so callers can treat the muxer
// as a transparent pass-through.
type Muxer struct {
	FFmpegPath string
	// WorkDir is where muxed .av.mp4 files are written. The motion target
	// dir is read-only to the bot user, so output cannot live next to the
	// inputs. Defaults to os.TempDir() when empty.
	WorkDir string
	Timeout time.Duration

	runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// New returns a Muxer that invokes the given ffmpeg binary.
func New(ffmpegPath string) *Muxer {
	return &Muxer{
		FFmpegPath: ffmpegPath,
		Timeout:    30 * time.Second,
		runFunc:    defaultRun,
	}
}

// Mux looks for <basename>.wav next to videoPath. If found, it produces
// <basename>.av.mp4 inside the muxer's WorkDir (defaults to os.TempDir())
// and returns its path. If absent, it returns videoPath unchanged. A nil
// receiver is a no-op pass-through.
func (m *Muxer) Mux(ctx context.Context, videoPath string) (string, error) {
	if m == nil {
		return videoPath, nil
	}
	wavPath := siblingWAV(videoPath)
	if _, err := os.Stat(wavPath); err != nil {
		if os.IsNotExist(err) {
			return videoPath, nil
		}
		return "", fmt.Errorf("audio stat: %w", err)
	}

	workDir := m.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	outPath := filepath.Join(workDir, base+".av.mp4")

	runCtx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()

	out, err := m.runFunc(runCtx, m.FFmpegPath,
		"-y",
		"-i", videoPath,
		"-i", wavPath,
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "96k",
		"-shortest",
		outPath,
	)
	if err != nil {
		return "", fmt.Errorf("ffmpeg mux: %w: %s", err, string(out))
	}
	return outPath, nil
}

func siblingWAV(videoPath string) string {
	return strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".wav"
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
