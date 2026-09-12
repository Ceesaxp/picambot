// Package homekit exposes camera monitoring as a single native HAP switch.
package homekit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
)

type Controller interface {
	Monitoring() bool
	SetMonitoring(context.Context, bool) error
	MonitoringChanges() <-chan struct{}
}

type Config struct {
	Enabled    bool
	Name       string
	PIN        string
	StorageDir string
	Address    string
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.PIN) != 8 {
		return fmt.Errorf("HOMEKIT_PIN must contain eight digits")
	}
	for _, r := range c.PIN {
		if r < '0' || r > '9' {
			return fmt.Errorf("HOMEKIT_PIN must contain eight digits")
		}
	}
	if hap.InvalidPins[c.PIN] {
		return fmt.Errorf("HOMEKIT_PIN is not an allowed pairing code")
	}
	return nil
}

type Switch struct {
	server     *hap.Server
	accessory  *accessory.Switch
	controller Controller
	// Serialize remote writes and notifications through the entire HAP update,
	// including the library's assignment after SetValueRequestFunc returns.
	mu sync.Mutex
}

func New(ctx context.Context, cfg Config, controller Controller, logger *slog.Logger) (*Switch, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.StorageDir, 0o700); err != nil {
		return nil, err
	}
	a := accessory.NewSwitch(accessory.Info{Name: cfg.Name, Manufacturer: "PiCam Bot", Model: "Monitoring switch", SerialNumber: "picambot-monitoring"})
	// HAP v0.0.35's ordinary On characteristic suppresses equal-value writes.
	// HoldPosition supplies a Bool with equal-value delivery enabled. Reuse that
	// behavior but expose only the standard On type and permissions on the wire.
	on := characteristic.NewHoldPosition().Bool
	on.Type = characteristic.TypeOn
	on.Permissions = a.Switch.On.Permissions
	for i, c := range a.Switch.Cs {
		if c == a.Switch.On.C {
			a.Switch.Cs[i] = on.C
		}
	}
	a.Switch.On = &characteristic.On{Bool: on}
	on.SetValue(controller.Monitoring())
	s := &Switch{accessory: a, controller: controller}
	on.ValueRequestFunc = func(*http.Request) (interface{}, int) { return controller.Monitoring(), 0 }
	on.SetValueRequestFunc = func(v interface{}, req *http.Request) (interface{}, int) {
		s.mu.Lock()
		opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := controller.SetMonitoring(opCtx, v.(bool)); err != nil {
			on.SetValue(controller.Monitoring())
			s.mu.Unlock()
			logger.Error("HomeKit monitoring command failed", "on", v, "err", err)
			return nil, -70402
		}
		// OnValueRemoteUpdate runs after HAP commits the requested value.
		return nil, 0
	}
	on.OnValueRemoteUpdate(func(bool) {
		on.SetValue(controller.Monitoring())
		s.mu.Unlock()
	})
	server, err := hap.NewServer(hap.NewFsStore(cfg.StorageDir), a.A)
	if err != nil {
		return nil, err
	}
	server.Pin = cfg.PIN
	server.Addr = cfg.Address
	s.server = server
	return s, nil
}

func (s *Switch) syncState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Avoid redundant Home notifications from the equal-value characteristic.
	value := s.controller.Monitoring()
	if s.accessory.Switch.On.Value() != value {
		s.accessory.Switch.On.SetValue(value)
	}
}

func (s *Switch) Run(ctx context.Context) error {
	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-syncCtx.Done():
				return
			case <-s.controller.MonitoringChanges():
				s.syncState()
			}
		}
	}()
	err := s.server.ListenAndServe(ctx)
	cancel()
	<-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
