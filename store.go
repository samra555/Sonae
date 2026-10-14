package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
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

// Writing to a temp file and renaming it means a crash mid-write cannot corrupt the file.
func writeJSONAtomically(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, encoded, 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// legacyData mirrors the fields of older file formats that Action no longer has: first a single
// "multiplier" with its source, later "my_multiplier" and "llm_multiplier".
type legacyData struct {
	Cases []struct {
		Kind            Kind    `json:"kind"`
		BaseProbability float64 `json:"base_probability"`
		Actions         []struct {
			Multiplier       *float64 `json:"multiplier"`
			MultiplierSource string   `json:"multiplier_source"`
			MyMultiplier     *float64 `json:"my_multiplier"`
			LLMMultiplier    *float64 `json:"llm_multiplier"`
		} `json:"actions"`
	} `json:"cases"`
}

// migrateLegacyMultipliers turns the multipliers of older files into weights. For a risk the weight
// is the same change at the base probability: base 10% and ×0.9 become −1 point, so current
// probabilities stay as they were. An aspiration's multiplier has no weight equivalent (a weight
// is a share of the goal there), so it is dropped. Files in the new format have no such keys.
func migrateLegacyMultipliers(rawBytes []byte, data *Data) error {
	var legacy legacyData
	if err := json.Unmarshal(rawBytes, &legacy); err != nil {
		return err
	}
	for caseIndex, legacyCase := range legacy.Cases {
		if legacyCase.Kind != KindRisk {
			continue
		}
		for actionIndex, legacyAction := range legacyCase.Actions {
			myMultiplier, llmMultiplier := legacyAction.MyMultiplier, legacyAction.LLMMultiplier
			if legacyAction.Multiplier != nil {
				if legacyAction.MultiplierSource == "нейросеть" {
					llmMultiplier = cmp.Or(llmMultiplier, legacyAction.Multiplier)
				} else {
					myMultiplier = cmp.Or(myMultiplier, legacyAction.Multiplier)
				}
			}

			action := &data.Cases[caseIndex].Actions[actionIndex]
			action.Weight = cmp.Or(action.Weight, weightFromMultiplier(legacyCase.BaseProbability, myMultiplier))
			action.LLMWeight = cmp.Or(action.LLMWeight, weightFromMultiplier(legacyCase.BaseProbability, llmMultiplier))
		}
	}
	return nil
}

func weightFromMultiplier(baseProbability float64, multiplier *float64) *float64 {
	if multiplier == nil {
		return nil
	}
	weight := math.Round(baseProbability*(*multiplier-1)*100*100) / 100
	return &weight
}
