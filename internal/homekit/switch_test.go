package homekit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

type fakeController struct {
	on      bool
	calls   []bool
	err     error
	changes chan struct{}
}

func (f *fakeController) Monitoring() bool                   { return f.on }
func (f *fakeController) MonitoringChanges() <-chan struct{} { return f.changes }
func (f *fakeController) SetMonitoring(_ context.Context, on bool) error {
	f.calls = append(f.calls, on)
	if f.err != nil {
		return f.err
	}
	f.on = on
	return nil
}
func TestSwitchWritesAndStatus(t *testing.T) {
	f := &fakeController{changes: make(chan struct{}, 1)}
	cfg := Config{Enabled: true, Name: "Camera monitoring", PIN: "24681357", StorageDir: t.TempDir()}
	s, err := New(context.Background(), cfg, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	on := s.accessory.Switch.On
	req := httptest.NewRequest("PUT", "/characteristics", nil)
	for _, v := range []bool{false, true, true, false, false} {
		if _, code := on.SetValueRequest(v, req); code != 0 {
			t.Fatalf("write %v: %d", v, code)
		}
	}
	if len(f.calls) != 5 {
		t.Fatalf("equal-value commands skipped: %v", f.calls)
	}
	f.on = true
	s.syncState()
	if !on.Value() {
		t.Fatal("external state not pushed")
	}
	f.err = errors.New("stop failed")
	if _, code := on.SetValueRequest(false, req); code != -70402 {
		t.Fatalf("failure code=%d", code)
	}
	if !on.Value() {
		t.Fatal("failed command changed switch value")
	}
	f.err = nil
	if _, code := on.SetValueRequest(false, req); code != 0 {
		t.Fatal("retry failed")
	}
	f.on = true
	if value, code := on.ValueRequest(req); code != 0 || value != true {
		t.Fatal("read did not use controller state")
	}
	// Pairing identity remains stable when the adapter is reconstructed.
	s2, err := New(context.Background(), cfg, f, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if s2.accessory.Id != s.accessory.Id {
		t.Fatal("accessory ID changed")
	}
}
