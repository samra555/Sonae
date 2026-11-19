package models_test

import (
	"math"
	"strings"
	"testing"

	"gotovnost/internal/models"
)

func validData() *models.Data {
	return &models.Data{
		Profile: models.Profile{Age: 30, Sex: "male", Country: "Россия"},
		Cases: []models.Case{
			{
				ID: "c1", Kind: models.KindRisk, Title: "Риск", Horizon: "1 год",
				BaseProbability: 0.3, BaseSource: "оценка",
				Actions: []models.Action{
					{ID: "a1", Title: "Действие 1"},
				},
			},
		},
	}
}

func TestValidateOK(t *testing.T) {
	if err := validData().Validate(); err != nil {
		t.Fatalf("valid data rejected: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(d *models.Data)
	}{
		{"empty case id", func(d *models.Data) { d.Cases[0].ID = "" }},
		{"duplicate case id", func(d *models.Data) {
			d.Cases = append(d.Cases, models.Case{ID: "c1", Kind: models.KindRisk, Title: "Другой"})
		}},
		{"empty title", func(d *models.Data) { d.Cases[0].Title = " " }},
		{"bad kind", func(d *models.Data) { d.Cases[0].Kind = models.Kind("wat") }},
		{"bad base probability", func(d *models.Data) { d.Cases[0].BaseProbability = 1.5 }},
		{"bad weight", func(d *models.Data) {
			d.Cases[0].Actions[0].Weight = f(200)
		}},
		{"unknown requires link", func(d *models.Data) {
			d.Cases[0].Actions[0].Requires = []string{"missing"}
		}},
		{"duplicate action id", func(d *models.Data) {
			d.Cases[0].Actions = append(d.Cases[0].Actions, models.Action{ID: "a1", Title: "Дубль"})
		}},
		{"empty step title", func(d *models.Data) {
			d.Cases[0].Actions[0].Steps = []models.Step{{ID: "s1", Title: " "}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validData()
			tc.mutate(d)
			if err := d.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestValidateRequiresCycle(t *testing.T) {
	d := validData()
	d.Cases[0].Actions = []models.Action{
		{ID: "a1", Title: "Один", Requires: []string{"a2"}},
		{ID: "a2", Title: "Два", Requires: []string{"a1"}},
	}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "циклическая") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestCurrentProbability(t *testing.T) {
	c := models.Case{BaseProbability: 0.3, Actions: []models.Action{
		{Weight: f(-10), Done: true},
		{Weight: f(-5), Done: false},
	}}
	if got := c.CurrentProbability(); math.Abs(got-0.2) > 1e-9 {
		t.Fatalf("CurrentProbability = %v, want 0.2", got)
	}
}

func TestCurrentProbabilityClamped(t *testing.T) {
	c := models.Case{BaseProbability: 0.9, Actions: []models.Action{{Weight: f(100), Done: true}}}
	if got := c.CurrentProbability(); got != 1.0 {
		t.Fatalf("CurrentProbability = %v, want 1.0 (clamped)", got)
	}
}

func TestCompletion(t *testing.T) {
	c := models.Case{Kind: models.KindAspiration, Actions: []models.Action{
		{Weight: f(60), Done: true},
		{Weight: f(40), Done: false},
	}}
	if got := c.Completion(); got != 0.6 {
		t.Fatalf("Completion = %v, want 0.6", got)
	}
}

func TestWeightSum(t *testing.T) {
	c := models.Case{Actions: []models.Action{{Weight: f(30)}, {Weight: f(70)}, {}}}
	if got := c.WeightSum(); got != 100 {
		t.Fatalf("WeightSum = %v, want 100", got)
	}
}

func TestBlockReasonRequires(t *testing.T) {
	d := &models.Data{Cases: []models.Case{{
		ID: "c1", Kind: models.KindAspiration,
		Actions: []models.Action{
			{ID: "a1", Title: "Сначала"},
			{ID: "a2", Title: "Потом", Requires: []string{"a1"}},
		},
	}}}
	reason := d.BlockReason(&d.Cases[0].Actions[1])
	if !strings.Contains(reason, "Сначала") {
		t.Fatalf("BlockReason = %q, want to mention required action", reason)
	}
}

func TestBlockReasonExcludes(t *testing.T) {
	d := &models.Data{Cases: []models.Case{{
		ID: "c1", Kind: models.KindAspiration,
		Actions: []models.Action{
			{ID: "a1", Title: "Одно", Done: true},
			{ID: "a2", Title: "Другое", Excludes: []string{"a1"}},
		},
	}}}
	reason := d.BlockReason(&d.Cases[0].Actions[1])
	if !strings.Contains(reason, "несовместимо") {
		t.Fatalf("BlockReason = %q, want incompatibility", reason)
	}
}

func TestToggleAction(t *testing.T) {
	d := &models.Data{Cases: []models.Case{{
		ID: "c1", Kind: models.KindAspiration,
		Actions: []models.Action{{ID: "a1", Title: "Один"}},
	}}}
	action := &d.Cases[0].Actions[0]
	if err := d.ToggleAction(action); err != nil || !action.Done {
		t.Fatalf("first toggle failed: %v, done=%v", err, action.Done)
	}
	if err := d.ToggleAction(action); err != nil || action.Done {
		t.Fatalf("second toggle failed: %v, done=%v", err, action.Done)
	}
}

func TestToggleActionBlocked(t *testing.T) {
	d := &models.Data{Cases: []models.Case{{
		ID: "c1", Kind: models.KindAspiration,
		Actions: []models.Action{
			{ID: "a1", Title: "Сначала"},
			{ID: "a2", Title: "Потом", Requires: []string{"a1"}},
		},
	}}}
	err := d.ToggleAction(&d.Cases[0].Actions[1])
	if err == nil {
		t.Fatal("expected blocked action error")
	}
}

func TestToggleStepCompletesAction(t *testing.T) {
	d := &models.Data{Cases: []models.Case{{
		ID: "c1", Kind: models.KindAspiration,
		Actions: []models.Action{{ID: "a1", Title: "Один", Steps: []models.Step{{ID: "s1", Title: "Шаг"}}}},
	}}}
	action := &d.Cases[0].Actions[0]
	step := &action.Steps[0]
	if err := d.ToggleStep(action, step); err != nil {
		t.Fatalf("toggle step: %v", err)
	}
	if !step.Done || !action.Done {
		t.Fatalf("expected step and action done, step=%v action=%v", step.Done, action.Done)
	}
}

func TestToggleStepReopensAction(t *testing.T) {
	d := &models.Data{Cases: []models.Case{{
		ID: "c1", Kind: models.KindAspiration,
		Actions: []models.Action{{ID: "a1", Title: "Один", Done: true, Steps: []models.Step{{ID: "s1", Title: "Шаг", Done: true}}}},
	}}}
	action := &d.Cases[0].Actions[0]
	step := &action.Steps[0]
	if err := d.ToggleStep(action, step); err != nil {
		t.Fatalf("toggle step: %v", err)
	}
	if step.Done || action.Done {
		t.Fatalf("expected step and action undone")
	}
}

func TestDoneCountAndProgress(t *testing.T) {
	c := models.Case{Actions: []models.Action{{Done: true}, {Done: false}, {Done: true}}}
	if c.DoneCount() != 2 {
		t.Fatalf("DoneCount = %d, want 2", c.DoneCount())
	}
	if c.ProgressPercent() != 66 {
		t.Fatalf("ProgressPercent = %d, want 66", c.ProgressPercent())
	}
}
