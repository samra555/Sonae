package models_test

import (
	"testing"

	"gotovnost/internal/models"
)

func TestValidWeight(t *testing.T) {
	cases := []struct {
		name   string
		kind   models.Kind
		weight *float64
		want   bool
	}{
		{"risk nil", models.KindRisk, nil, true},
		{"risk -100", models.KindRisk, f(-100), true},
		{"risk 100", models.KindRisk, f(100), true},
		{"risk -101", models.KindRisk, f(-101), false},
		{"risk 101", models.KindRisk, f(101), false},
		{"aspiration nil", models.KindAspiration, nil, true},
		{"aspiration 0", models.KindAspiration, f(0), true},
		{"aspiration 100", models.KindAspiration, f(100), true},
		{"aspiration -1", models.KindAspiration, f(-1), false},
		{"aspiration 101", models.KindAspiration, f(101), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := models.ValidWeight(tc.kind, tc.weight); got != tc.want {
				t.Fatalf("ValidWeight(%s, %v) = %v, want %v", tc.kind, tc.weight, got, tc.want)
			}
		})
	}
}

func TestWeightError(t *testing.T) {
	if models.WeightError(models.KindRisk) == nil || models.WeightError(models.KindAspiration) == nil {
		t.Fatal("expected non-nil errors")
	}
	if models.WeightError(models.KindRisk).Error() == models.WeightError(models.KindAspiration).Error() {
		t.Fatal("expected different messages for risk and aspiration")
	}
}

func TestPctLabel(t *testing.T) {
	cases := []struct {
		value *float64
		want  string
	}{
		{nil, "—"},
		{f(30), "+30%"},
		{f(-10), "−10%"},
		{f(0), "0%"},
	}
	for _, tc := range cases {
		if got := models.PctLabel(tc.value); got != tc.want {
			t.Errorf("PctLabel(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestWeightLabel(t *testing.T) {
	if got := models.WeightLabel(models.KindRisk, f(-10)); got != "−10%" {
		t.Errorf("risk weight label = %q, want −10%%", got)
	}
	if got := models.WeightLabel(models.KindAspiration, f(25)); got != "25%" {
		t.Errorf("aspiration weight label = %q, want 25%%", got)
	}
	if got := models.WeightLabel(models.KindRisk, nil); got != "—" {
		t.Errorf("nil weight label = %q, want —", got)
	}
}
