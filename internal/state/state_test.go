package state

import (
	"testing"
)

// UT-01: All valid transitions succeed.
func TestFSM_ValidTransitions(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		{StateSleep, StateMonitoring},
		{StateMonitoring, StateRecording},
		{StateMonitoring, StateDetecting},
		{StateMonitoring, StateSleep},
		{StateRecording, StateMonitoring},
		{StateRecording, StateSleep},
		{StateDetecting, StateMonitoring},
		{StateDetecting, StateSleep},
	}

	for _, tc := range tests {
		fsm := NewFSM(tc.from)
		if err := fsm.Transition(tc.to); err != nil {
			t.Errorf("Transition(%s -> %s): unexpected error: %v", tc.from, tc.to, err)
		}
		if fsm.Current() != tc.to {
			t.Errorf("Transition(%s -> %s): current = %s", tc.from, tc.to, fsm.Current())
		}
	}
}

// UT-02: Invalid transitions return ErrInvalidTransition.
func TestFSM_InvalidTransitions(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		{StateSleep, StateSleep},        // /stop from sleep
		{StateSleep, StateRecording},
		{StateSleep, StateDetecting},
		{StateMonitoring, StateMonitoring},
		{StateRecording, StateDetecting},
		{StateDetecting, StateRecording},
	}

	for _, tc := range tests {
		fsm := NewFSM(tc.from)
		err := fsm.Transition(tc.to)
		if err == nil {
			t.Errorf("Transition(%s -> %s): expected ErrInvalidTransition, got nil", tc.from, tc.to)
		}
		if err != ErrInvalidTransition {
			t.Errorf("Transition(%s -> %s): expected ErrInvalidTransition, got %v", tc.from, tc.to, err)
		}
		// State must not change on failed transition.
		if fsm.Current() != tc.from {
			t.Errorf("Transition(%s -> %s): state changed to %s after error", tc.from, tc.to, fsm.Current())
		}
	}
}

func TestFSM_Set(t *testing.T) {
	fsm := NewFSM(StateSleep)
	fsm.Set(StateDetecting)
	if fsm.Current() != StateDetecting {
		t.Errorf("Set: current = %s", fsm.Current())
	}
}
