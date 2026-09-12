package state

import (
	"errors"
	"sync"
)

// State represents one of the four FSM states.
type State string

const (
	StateSleep      State = "sleep"
	StateMonitoring State = "monitoring"
	StateRecording  State = "recording"
	StateDetecting  State = "detecting"
)

// ErrInvalidTransition is returned when a transition is not permitted.
var ErrInvalidTransition = errors.New("invalid state transition")

// transitions defines the allowed next states for each current state.
var transitions = map[State][]State{
	StateSleep:      {StateMonitoring},
	StateMonitoring: {StateRecording, StateDetecting, StateSleep},
	StateRecording:  {StateMonitoring, StateSleep},
	StateDetecting:  {StateMonitoring, StateSleep},
}

// FSM is a thread-safe finite state machine.
type FSM struct {
	current State
	mu      sync.Mutex
}

// NewFSM creates a new FSM with the given initial state.
func NewFSM(initial State) *FSM {
	return &FSM{current: initial}
}

// Transition moves to next if the transition is valid, otherwise returns ErrInvalidTransition.
func (f *FSM) Transition(next State) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	allowed := transitions[f.current]
	for _, s := range allowed {
		if s == next {
			f.current = next
			return nil
		}
	}
	return ErrInvalidTransition
}

// Current returns the current state.
func (f *FSM) Current() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

// Set forcibly sets the state without checking transitions. Used during startup
// to restore persisted state.
func (f *FSM) Set(s State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = s
}
