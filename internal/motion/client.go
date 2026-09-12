package motion

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ErrOutOfRange is returned when a recording duration is outside [1, 300].
var ErrOutOfRange = errors.New("recording duration out of range (1–300 seconds)")

// MotionClient controls the motion daemon.
type MotionClient interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Snapshot(ctx context.Context) (string, error)
	Record(ctx context.Context, seconds int) (string, error)
	DetectionStart(ctx context.Context) error
	DetectionPause(ctx context.Context) error
}

// HTTPMotionClient is the production implementation backed by motion's webcontrol API.
type HTTPMotionClient struct {
	baseURL    string
	targetDir  string
	httpClient *http.Client
}

// NewHTTPMotionClient constructs a client targeting host:port with output in targetDir.
func NewHTTPMotionClient(host string, port int, targetDir string) *HTTPMotionClient {
	return &HTTPMotionClient{
		baseURL:   fmt.Sprintf("http://%s:%d", host, port),
		targetDir: targetDir,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start starts the motion daemon via systemctl, then waits up to 5s for the
// webcontrol port to become reachable.
func (c *HTTPMotionClient) Start(ctx context.Context) error {
	if err := runSystemctl(ctx, "start"); err != nil {
		return err
	}
	return c.waitReachable(ctx, 5*time.Second)
}

// Stop stops the motion daemon via systemctl.
func (c *HTTPMotionClient) Stop(ctx context.Context) error {
	return runSystemctl(ctx, "stop")
}

// Snapshot triggers a snapshot and returns the path of the resulting JPEG.
func (c *HTTPMotionClient) Snapshot(ctx context.Context) (string, error) {
	before := time.Now()
	if err := c.get(ctx, "/0/action/snapshot"); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	return c.waitForFile(ctx, "*.jpg", before, 3*time.Second)
}

// Record records for seconds seconds and returns the path of the resulting MP4.
// Returns ErrOutOfRange if seconds is outside [1, 300].
func (c *HTTPMotionClient) Record(ctx context.Context, seconds int) (string, error) {
	if seconds < 1 || seconds > 300 {
		return "", ErrOutOfRange
	}

	before := time.Now()
	if err := c.get(ctx, "/0/action/eventstart"); err != nil {
		return "", fmt.Errorf("record start: %w", err)
	}

	select {
	case <-time.After(time.Duration(seconds) * time.Second):
	case <-ctx.Done():
		// Best-effort stop even if context is cancelled.
		_ = c.get(context.Background(), "/0/action/eventend") //nolint:contextcheck
		return "", ctx.Err()
	}

	if err := c.get(ctx, "/0/action/eventend"); err != nil {
		return "", fmt.Errorf("record stop: %w", err)
	}

	path, err := c.waitForFile(ctx, "*.mp4", before, 5*time.Second)
	if err != nil {
		return "", err
	}

	// Wait for motion to finalize the file (write the moov atom).
	if err := WaitFileStable(ctx, path, 10*time.Second); err != nil {
		return "", fmt.Errorf("record finalize: %w", err)
	}
	return path, nil
}

// DetectionStart resumes motion detection.
func (c *HTTPMotionClient) DetectionStart(ctx context.Context) error {
	return c.get(ctx, "/0/detection/start")
}

// DetectionPause pauses motion detection.
func (c *HTTPMotionClient) DetectionPause(ctx context.Context) error {
	return c.get(ctx, "/0/detection/pause")
}

// get performs a GET to the given path on the webcontrol API.
func (c *HTTPMotionClient) get(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("motion webcontrol returned %d", resp.StatusCode)
	}
	return nil
}

// waitReachable polls the webcontrol root until it responds or the timeout elapses.
func (c *HTTPMotionClient) waitReachable(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/0/detection/status", nil)
		resp, err := c.httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("motion webcontrol not reachable within %s", timeout)
}

// WaitFileStable polls until the file's size stops changing for two consecutive
// checks, meaning motion has finished writing and closed the file.
func WaitFileStable(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastSize int64 = -1
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > 0 && info.Size() == lastSize {
			return nil
		}
		lastSize = info.Size()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("file %s did not stabilize within %s", path, timeout)
}

// waitForFile polls targetDir for the newest file matching pattern that appeared
// after the given timestamp, returning its path or an error on timeout.
func (c *HTTPMotionClient) waitForFile(ctx context.Context, pattern string, after time.Time, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		matches, err := filepath.Glob(filepath.Join(c.targetDir, pattern))
		if err != nil {
			return "", err
		}

		// Filter to files created after the snapshot call.
		var candidates []string
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				continue
			}
			if info.ModTime().After(after) {
				candidates = append(candidates, m)
			}
		}

		if len(candidates) > 0 {
			// Return the most recently modified file.
			sort.Slice(candidates, func(i, j int) bool {
				ii, _ := os.Stat(candidates[i])
				jj, _ := os.Stat(candidates[j])
				return ii.ModTime().After(jj.ModTime())
			})
			return candidates[0], nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("no %s file appeared in %s within %s", pattern, c.targetDir, timeout)
}
