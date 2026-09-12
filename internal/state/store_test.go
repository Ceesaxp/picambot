package state

import (
	"path/filepath"
	"testing"
)

// UT-03: Save → Load round-trips all State values and chatID.
func TestPersistentStateStore_RoundTrip(t *testing.T) {
	tests := []struct {
		state  State
		chatID int64
	}{
		{StateSleep, 0},
		{StateMonitoring, 123456},
		{StateRecording, 999},
		{StateDetecting, 42},
	}

	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			dir := t.TempDir()
			store := NewPersistentStateStore(filepath.Join(dir, "state.json"))

			if err := store.Save(tc.state, tc.chatID); err != nil {
				t.Fatalf("Save: %v", err)
			}

			got, gotChatID, err := store.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got != tc.state {
				t.Errorf("state: got %q, want %q", got, tc.state)
			}
			if gotChatID != tc.chatID {
				t.Errorf("chatID: got %d, want %d", gotChatID, tc.chatID)
			}
		})
	}
}

func TestPersistentStateStore_MissingFile(t *testing.T) {
	dir := t.TempDir()
	store := NewPersistentStateStore(filepath.Join(dir, "nonexistent.json"))

	st, chatID, err := store.Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if st != StateSleep {
		t.Errorf("state: got %q, want %q", st, StateSleep)
	}
	if chatID != 0 {
		t.Errorf("chatID: got %d, want 0", chatID)
	}
}

func TestPersistentStateStore_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	store := NewPersistentStateStore(filepath.Join(dir, "subdir", "state.json"))

	if err := store.Save(StateMonitoring, 1); err != nil {
		t.Fatalf("Save to nested path: %v", err)
	}
	st, _, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st != StateMonitoring {
		t.Errorf("state: got %q", st)
	}
}
