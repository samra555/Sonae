package services_test

import (
	"testing"

	"gotovnost/internal/services"
)

func f(v float64) *float64 { return &v }

func promptTemplates(t *testing.T) *services.ParsedPrompts {
	t.Helper()
	prompts, err := services.ParsePrompts()
	if err != nil {
		t.Fatalf("ParsePrompts: %v", err)
	}
	return prompts
}
