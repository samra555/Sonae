package services_test

import (
	"strings"
	"testing"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

func TestBuildPromptRisk(t *testing.T) {
	prompts := promptTemplates(t)
	c := models.Case{ID: "c1", Kind: models.KindRisk, Title: "Риск", Horizon: "1 год", BaseProbability: 0.3}
	got, err := services.BuildPrompt(prompts.Estimate, models.Profile{Age: 30}, c)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if !strings.Contains(got, "estimate_action_weights") {
		t.Error("prompt should mention the task")
	}
	if !strings.Contains(got, "base_probability_percent") {
		t.Error("risk prompt should include base probability")
	}
}

func TestBuildPromptAspiration(t *testing.T) {
	prompts := promptTemplates(t)
	c := models.Case{ID: "c1", Kind: models.KindAspiration, Title: "Цель", Horizon: "1 год"}
	got, err := services.BuildPrompt(prompts.Estimate, models.Profile{Age: 30}, c)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if strings.Contains(got, "base_probability_percent") {
		t.Error("aspiration prompt should not include base probability")
	}
}

func TestParseAssistantReply(t *testing.T) {
	raw := "```json\n{\"case_id\":\"c1\",\"actions\":[],\"suggested_actions\":[],\"comment\":\"ok\"}\n```"
	reply, err := services.ParseAssistantReply(raw)
	if err != nil {
		t.Fatalf("ParseAssistantReply: %v", err)
	}
	if reply.CaseID != "c1" || reply.Comment != "ok" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestParseAssistantReplyRejectsUnknownField(t *testing.T) {
	raw := `{"case_id":"c1","actions":[],"suggested_actions":[],"comment":"ok","bogus":1}`
	if _, err := services.ParseAssistantReply(raw); err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestParseAssistantReplyNoJSON(t *testing.T) {
	if _, err := services.ParseAssistantReply("no json here"); err == nil {
		t.Fatal("expected no-JSON error")
	}
}

func TestAssistantReplyValidate(t *testing.T) {
	c := models.Case{ID: "c1", Kind: models.KindRisk, Actions: []models.Action{{ID: "a1", Title: "Один"}}}

	valid := services.AssistantReply{CaseID: "c1", Actions: []services.ActionEstimate{{ID: "a1", Confidence: "high"}}}
	if err := valid.Validate(c); err != nil {
		t.Fatalf("valid reply rejected: %v", err)
	}

	if err := (services.AssistantReply{CaseID: "wrong"}).Validate(c); err == nil {
		t.Error("expected case_id mismatch error")
	}
	if err := (services.AssistantReply{CaseID: "c1", Actions: []services.ActionEstimate{{ID: "unknown", Confidence: "high"}}}).Validate(c); err == nil {
		t.Error("expected unknown action error")
	}
	if err := (services.AssistantReply{CaseID: "c1", Actions: []services.ActionEstimate{{ID: "a1", Confidence: "wat"}}}).Validate(c); err == nil {
		t.Error("expected bad confidence error")
	}
	if err := (services.AssistantReply{CaseID: "c1", Actions: []services.ActionEstimate{{ID: "a1", Confidence: "high", Weight: f(200)}}}).Validate(c); err == nil {
		t.Error("expected bad weight error")
	}
}

func TestApplyReply(t *testing.T) {
	c := models.Case{ID: "c1", Kind: models.KindAspiration, Actions: []models.Action{
		{ID: "a1", Title: "Один"},
		{ID: "a2", Title: "Два"},
	}}
	reply := services.AssistantReply{
		CaseID: "c1",
		Actions: []services.ActionEstimate{
			{ID: "a1", Weight: f(40), Confidence: "high", Reasoning: "почему", SuggestedSteps: []string{"Шаг A", "Шаг B"}},
			{ID: "a2", Weight: f(60), Confidence: "medium"},
		},
		SuggestedActions: []services.SuggestedAction{{Title: "Новое", Weight: f(10), Confidence: "low"}},
	}
	selection := services.ApplySelection{
		ActionIDs:        map[string]bool{"a1": true, "a2": true},
		SuggestedIndexes: map[int]bool{0: true},
		StepKeys:         map[string]bool{services.StepKey("a1", 0): true},
	}
	services.ApplyReply(&c, reply, selection, "2026-11-17")

	if c.Actions[0].LLMWeight == nil || *c.Actions[0].LLMWeight != 40 {
		t.Errorf("a1 llm weight not applied")
	}
	if c.Actions[0].Confidence != "high" || c.Actions[0].Reasoning != "почему" {
		t.Errorf("a1 metadata not applied")
	}
	if len(c.Actions[0].Steps) != 1 || c.Actions[0].Steps[0].Title != "Шаг A" {
		t.Errorf("a1 steps = %+v, want only 'Шаг A'", c.Actions[0].Steps)
	}
	if c.Actions[1].LLMWeight == nil || *c.Actions[1].LLMWeight != 60 {
		t.Errorf("a2 llm weight not applied")
	}
	if len(c.Actions) != 3 || c.Actions[2].Title != "Новое" {
		t.Errorf("suggested action not added: %+v", c.Actions)
	}
}

func TestEstimateAndRefineMutuallyExclusive(t *testing.T) {
	estimate := `{"case_id":"c1","actions":[],"suggested_actions":[],"comment":""}`
	if _, err := services.ParseRefineReply(estimate); err == nil {
		t.Error("estimate reply should be rejected by refine parser")
	}

	refine := `{"action_id":"a1","step_id":"s1","steps":["x"]}`
	if _, err := services.ParseAssistantReply(refine); err == nil {
		t.Error("refine reply should be rejected by estimate parser")
	}
}
