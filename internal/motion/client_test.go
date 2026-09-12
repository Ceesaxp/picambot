package motion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// UT-04: Snapshot() calls the correct webcontrol URL and returns a file path.
func TestHTTPMotionClient_Snapshot(t *testing.T) {
	dir := t.TempDir()
	var snapshotCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/0/action/snapshot" {
			snapshotCalled = true
			// Simulate motion writing the file after the request.
			go func() {
				time.Sleep(50 * time.Millisecond)
				_ = os.WriteFile(filepath.Join(dir, "snap.jpg"), []byte("jpeg"), 0o644)
			}()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv, dir)
	path, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !snapshotCalled {
		t.Error("snapshot webcontrol URL was not called")
	}
	if path == "" {
		t.Error("expected a file path, got empty string")
	}
}

// UT-05: Record(N > 300) returns ErrOutOfRange before any HTTP call.
func TestHTTPMotionClient_Record_OutOfRange(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv, t.TempDir())
	_, err := c.Record(context.Background(), 301)
	if err != ErrOutOfRange {
		t.Errorf("Record(301): expected ErrOutOfRange, got %v", err)
	}
	if called {
		t.Error("HTTP server was called despite out-of-range value")
	}

	_, err = c.Record(context.Background(), 0)
	if err != ErrOutOfRange {
		t.Errorf("Record(0): expected ErrOutOfRange, got %v", err)
	}
}

func newTestClient(srv *httptest.Server, dir string) *HTTPMotionClient {
	c := &HTTPMotionClient{
		baseURL:    srv.URL,
		targetDir:  dir,
		httpClient: srv.Client(),
	}
	return c
}
