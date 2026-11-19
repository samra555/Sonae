package models_test

import (
	"regexp"
	"testing"

	"gotovnost/internal/models"
)

func TestNewID(t *testing.T) {
	id := models.NewID("c")
	if matched, _ := regexp.MatchString(`^c-[0-9a-f]{8}$`, id); !matched {
		t.Fatalf("NewID(\"c\") = %q, want c-XXXXXXXX", id)
	}

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := models.NewID("a")
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
