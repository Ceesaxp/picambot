package audio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMux_NoSiblingWAV_ReturnsVideoUnchanged(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("ffmpeg")
	m.runFunc = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		t.Fatal("ffmpeg should not run when no WAV exists")
		return nil, nil
	}

	out, err := m.Mux(context.Background(), video)
	if err != nil {
		t.Fatalf("Mux: %v", err)
	}
	if out != video {
		t.Errorf("out = %q, want %q (unchanged)", out, video)
	}
}

func TestMux_WithSiblingWAV_ProducesAVFile(t *testing.T) {
	dir := t.TempDir()
	workDir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	wav := filepath.Join(dir, "clip.wav")
	wantOut := filepath.Join(workDir, "clip.av.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wav, []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}

	var capturedArgs []string
	m := New("ffmpeg")
	m.WorkDir = workDir
	m.runFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "ffmpeg" {
			t.Errorf("name = %q, want ffmpeg", name)
		}
		capturedArgs = args
		return nil, os.WriteFile(wantOut, []byte("av"), 0o644)
	}

	out, err := m.Mux(context.Background(), video)
	if err != nil {
		t.Fatalf("Mux: %v", err)
	}
	if out != wantOut {
		t.Errorf("out = %q, want %q", out, wantOut)
	}
	if _, err := os.Stat(wantOut); err != nil {
		t.Errorf("expected output file to exist: %v", err)
	}
	if !containsPair(capturedArgs, "-i", video) {
		t.Errorf("ffmpeg args missing -i %s: %v", video, capturedArgs)
	}
	if !containsPair(capturedArgs, "-i", wav) {
		t.Errorf("ffmpeg args missing -i %s: %v", wav, capturedArgs)
	}
	if len(capturedArgs) == 0 || capturedArgs[len(capturedArgs)-1] != wantOut {
		t.Errorf("ffmpeg output arg = %q, want %q", lastArg(capturedArgs), wantOut)
	}
}

func TestMux_FFmpegFailure_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	wav := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wav, []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New("ffmpeg")
	m.runFunc = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("invalid input"), errors.New("exit 1")
	}

	if _, err := m.Mux(context.Background(), video); err == nil {
		t.Fatal("Mux: expected error, got nil")
	}
}

func TestMux_NilReceiver_PassesThrough(t *testing.T) {
	var m *Muxer
	out, err := m.Mux(context.Background(), "/tmp/clip.mp4")
	if err != nil {
		t.Fatalf("Mux on nil: %v", err)
	}
	if out != "/tmp/clip.mp4" {
		t.Errorf("out = %q, want unchanged", out)
	}
}

func containsPair(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}
