package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// StateStore persists the FSM state across restarts.
type StateStore interface {
	Load() (State, int64, error) // returns State and chatID
	Save(State, int64) error
}

type stateFile struct {
	State     State     `json:"state"`
	ChatID    int64     `json:"chat_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PersistentStateStore is the file-backed implementation.
type PersistentStateStore struct {
	path string
}

// NewPersistentStateStore creates a store that reads/writes to path.
func NewPersistentStateStore(path string) *PersistentStateStore {
	return &PersistentStateStore{path: path}
}

// Load reads state from disk. Returns StateSleep and chatID=0 if the file does not exist.
func (s *PersistentStateStore) Load() (State, int64, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return StateSleep, 0, nil
	}
	if err != nil {
		return StateSleep, 0, err
	}

	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return StateSleep, 0, err
	}
	return sf.State, sf.ChatID, nil
}

// Save atomically writes state to disk via a temp file + rename.
func (s *PersistentStateStore) Save(st State, chatID int64) error {
	sf := stateFile{State: st, ChatID: chatID, UpdatedAt: time.Now()}
	data, err := json.Marshal(sf)
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
