package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type Store struct {
	path string
	mu   sync.RWMutex
	data Data
}

func NewStore(path string) (*Store, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}

	var data Data
	if err := json.Unmarshal(rawBytes, &data); err != nil {
		return nil, fmt.Errorf("%s: неверный JSON: %w", path, err)
	}
	if err := migrateLegacyMultipliers(rawBytes, &data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := data.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Store{path: path, data: data}, nil
}

// Read gives the callback a view of the current data. The callback must not modify it.
func (s *Store) Read(read func(data *Data)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	read(&s.data)
}

// Update mutates a copy of the data, validates it, writes it to disk, and only then
// replaces the live data. A failed mutation or validation therefore never leaves
// half-applied changes behind.
func (s *Store) Update(mutate func(data *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, err := cloneData(s.data)
	if err != nil {
		return err
	}
	if err := mutate(&draft); err != nil {
		return err
	}
	if err := draft.Validate(); err != nil {
		return err
	}
	if err := writeJSONAtomically(s.path, draft); err != nil {
		return err
	}
	s.data = draft
	return nil
}

func cloneData(data Data) (Data, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return Data{}, err
	}
	var clone Data
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return Data{}, err
	}
	return clone, nil
}

// Writing to a temp file and renaming it means a crash mid-write cannot corrupt data.json.
func writeJSONAtomically(path string, data Data) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, encoded, 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// legacyData mirrors the fields of the old file format that Action no longer has.
type legacyData struct {
	Cases []struct {
		Actions []struct {
			Multiplier       *float64 `json:"multiplier"`
			MultiplierSource string   `json:"multiplier_source"`
		} `json:"actions"`
	} `json:"cases"`
}

// migrateLegacyMultipliers moves an old "multiplier" into the new fields: a neural-net one becomes
// the stored estimate, anything else becomes the user's own estimate (unless one is already set).
// Files already in the new format have no such keys, so this does nothing for them.
func migrateLegacyMultipliers(rawBytes []byte, data *Data) error {
	var legacy legacyData
	if err := json.Unmarshal(rawBytes, &legacy); err != nil {
		return err
	}
	for caseIndex, legacyCase := range legacy.Cases {
		for actionIndex, legacyAction := range legacyCase.Actions {
			if legacyAction.Multiplier == nil {
				continue
			}
			action := &data.Cases[caseIndex].Actions[actionIndex]
			if legacyAction.MultiplierSource == "нейросеть" {
				action.LLMMultiplier = legacyAction.Multiplier
				continue
			}
			if action.MyMultiplier == nil {
				action.MyMultiplier = legacyAction.Multiplier
			}
		}
	}
	return nil
}
