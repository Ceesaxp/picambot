package bot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"picambot/internal/state"
)

// Monitoring reports whether motion detection is active.
func (b *Bot) Monitoring() bool { return b.fsm.Current() == state.StateDetecting }

// MonitoringChanges signals that clients should read the latest status. Signals
// are coalesced; the single HomeKit adapter consumes them.
func (b *Bot) MonitoringChanges() <-chan struct{} { return b.monitoringChanges }

// SetMonitoring implements the combined Home switch without sending Telegram
// replies or changing the persisted Telegram destination.
func (b *Bot) SetMonitoring(ctx context.Context, on bool) error {
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !on {
		if b.fsm.Current() == state.StateSleep {
			return b.persistState()
		}
		return b.stopCamera(ctx)
	}
	switch b.fsm.Current() {
	case state.StateDetecting:
		return b.persistState()
	case state.StateRecording:
		return fmt.Errorf("cannot enable detection during a recording")
	case state.StateSleep:
		if err := b.startCamera(ctx); err != nil {
			return err
		}
	}
	return b.enableDetection(ctx)
}

// The following operations require controlMu. Both Telegram and HomeKit use
// them, so hardware changes, persistence and status notifications stay ordered.
func (b *Bot) startCamera(ctx context.Context) error {
	if b.fsm.Current() != state.StateSleep {
		return state.ErrInvalidTransition
	}
	if err := b.motion.Start(ctx); err != nil {
		return fmt.Errorf("start camera: %w", err)
	}
	b.fsm.Set(state.StateMonitoring)
	return b.persistState()
}

func (b *Bot) stopCamera(ctx context.Context) error {
	if b.fsm.Current() == state.StateSleep {
		return state.ErrInvalidTransition
	}
	// Wait for cancellation cleanup before allowing another camera session.
	b.recordMu.Lock()
	if b.recordCancel != nil {
		b.recordCancel()
		if b.recordDone != nil {
			<-b.recordDone
		}
		b.recordCancel = nil
		b.fsm.Set(state.StateMonitoring)
	}
	b.recordMu.Unlock()
	// A failed daemon stop preserves the current state and watcher for a retry.
	if err := b.motion.Stop(ctx); err != nil {
		return errors.Join(fmt.Errorf("stop camera: %w", err), b.persistState())
	}
	b.stopWatcher()
	b.fsm.Set(state.StateSleep)
	return b.persistState()
}

func (b *Bot) enableDetection(ctx context.Context) error {
	if b.fsm.Current() != state.StateMonitoring {
		return state.ErrInvalidTransition
	}
	// Arm the consumer first so a watcher failure never enables detection.
	if err := b.startWatcher(b.targetDir); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	if err := b.motion.DetectionStart(ctx); err != nil {
		b.stopWatcher()
		return fmt.Errorf("enable detection: %w", err)
	}
	b.fsm.Set(state.StateDetecting)
	return b.persistState()
}

func (b *Bot) disableDetection(ctx context.Context) error {
	if b.fsm.Current() != state.StateDetecting {
		return state.ErrInvalidTransition
	}
	if err := b.motion.DetectionPause(ctx); err != nil {
		return fmt.Errorf("disable detection: %w", err)
	}
	b.stopWatcher()
	b.fsm.Set(state.StateMonitoring)
	return b.persistState()
}

func (b *Bot) persistState() error {
	select {
	case b.monitoringChanges <- struct{}{}:
	default:
	}
	if err := b.store.Save(b.fsm.Current(), b.sender.ChatID()); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// Recover reconciles persisted detection with Motion before accepting controls.
func (b *Bot) Recover(ctx context.Context) error {
	b.controlMu.Lock()
	defer b.controlMu.Unlock()
	switch b.fsm.Current() {
	case state.StateDetecting:
		b.fsm.Set(state.StateMonitoring)
		if err := b.motion.Start(ctx); err != nil {
			b.fsm.Set(state.StateSleep)
			return errors.Join(err, b.persistState())
		}
		if err := b.enableDetection(ctx); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return errors.Join(err, b.stopCamera(cleanupCtx))
		}
	case state.StateRecording:
		// A recording goroutine cannot survive a process restart.
		b.fsm.Set(state.StateMonitoring)
		return b.persistState()
	}
	return nil
}
