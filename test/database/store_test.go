package database_test

import (
	"os"
	"path/filepath"
	"testing"

	"gotovnost/internal/database"
	"gotovnost/internal/models"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func validJSON() string {
	return `{
  "profile": {"age": 30, "sex": "male", "country": "Россия"},
  "cases": [
    {"id": "c1", "kind": "risk", "title": "Риск", "horizon": "1 год",
     "base_probability": 0.3, "base_source": "оценка",
     "actions": [{"id": "a1", "title": "Действие"}]}
  ]
}`
}

func TestNewStoreLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeFile(t, path, validJSON())

	store, err := database.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	var cases int
	store.Read(func(data *models.Data) { cases = len(data.Cases) })
	if cases != 1 {
		t.Fatalf("expected 1 case, got %d", cases)
	}
}

func TestNewStoreRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeFile(t, path, "{not json")

	if _, err := database.NewStore(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewStoreRejectsInvalidData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeFile(t, path, `{"profile":{"age":30,"country":"Россия"},"cases":[{"id":"c1","kind":"risk","title":"","actions":[]}]}`)

	if _, err := database.NewStore(path); err == nil {
		t.Fatal("expected validation error for empty title")
	}
}

func TestUpdatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeFile(t, path, validJSON())

	store, err := database.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	err = store.Update(func(data *models.Data) error {
		data.Cases = append(data.Cases, models.Case{ID: "c2", Kind: models.KindAspiration, Title: "Цель", Horizon: "1 год", Actions: []models.Action{}})
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	reloaded, err := database.NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var count int
	reloaded.Read(func(data *models.Data) { count = len(data.Cases) })
	if count != 2 {
		t.Fatalf("expected 2 cases after update, got %d", count)
	}
}

func TestUpdateRejectsInvalidMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	writeFile(t, path, validJSON())

	store, err := database.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	err = store.Update(func(data *models.Data) error {
		data.Cases[0].Title = " "
		return nil
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestMigrateLegacyMultipliers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	legacy := `{
  "profile": {"age": 30, "sex": "male", "country": "Россия"},
  "cases": [
    {"id": "c1", "kind": "risk", "title": "Риск", "horizon": "1 год", "base_probability": 0.1,
     "actions": [
       {"id": "a1", "title": "Свой", "multiplier": 0.8, "multiplier_source": "сам"},
       {"id": "a2", "title": "Нейросети", "multiplier": 0.9, "multiplier_source": "нейросеть"}
     ]}
  ]
}`
	writeFile(t, path, legacy)

	store, err := database.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.Read(func(data *models.Data) {
		actions := data.Cases[0].Actions
		if actions[0].Weight == nil || *actions[0].Weight != -2 {
			t.Errorf("a1 weight = %v, want -2 (0.8 of 10%%)", actions[0].Weight)
		}
		if actions[1].LLMWeight == nil || *actions[1].LLMWeight != -1 {
			t.Errorf("a2 llm_weight = %v, want -1 (0.9 of 10%%)", actions[1].LLMWeight)
		}
		if actions[1].Weight != nil {
			t.Errorf("a2 weight should stay nil (neural net source)")
		}
	})
}
