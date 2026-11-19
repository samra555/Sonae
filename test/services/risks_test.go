package services_test

import (
	"strings"
	"testing"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

func TestBuildRisksPrompt(t *testing.T) {
	prompts := promptTemplates(t)
	data := &models.Data{
		Profile: models.Profile{Age: 30, Country: "Россия"},
		Cases:   []models.Case{{ID: "c1", Kind: models.KindRisk, Title: "Уже есть", Category: "здоровье"}},
	}
	got, err := services.BuildRisksPrompt(prompts.Risks, data, "переезд")
	if err != nil {
		t.Fatalf("BuildRisksPrompt: %v", err)
	}
	if !strings.Contains(got, "suggest_main_risks") {
		t.Error("prompt should mention the task")
	}
	if !strings.Contains(got, "Уже есть") {
		t.Error("prompt should list existing cases")
	}
	if !strings.Contains(got, "переезд") {
		t.Error("prompt should include focus")
	}
}

func TestParseRisksReply(t *testing.T) {
	raw := `{"risks":[{"title":"Риск","horizon":"1 год","base_probability_percent":30,"actions":[]}]}`
	reply, err := services.ParseRisksReply(raw)
	if err != nil {
		t.Fatalf("ParseRisksReply: %v", err)
	}
	if len(reply.Risks) != 1 || reply.Risks[0].Title != "Риск" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestRisksReplyValidate(t *testing.T) {
	valid := services.RisksReply{Risks: []services.SuggestedRisk{{Title: "Риск", Horizon: "1 год", BaseProbabilityPercent: f(30)}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid reply rejected: %v", err)
	}

	if err := (services.RisksReply{}).Validate(); err == nil {
		t.Error("expected empty risks error")
	}
	if err := (services.RisksReply{Risks: []services.SuggestedRisk{{Title: " ", Horizon: "1 год", BaseProbabilityPercent: f(30)}}}).Validate(); err == nil {
		t.Error("expected empty title error")
	}
	if err := (services.RisksReply{Risks: []services.SuggestedRisk{{Title: "Риск", Horizon: "", BaseProbabilityPercent: f(30)}}}).Validate(); err == nil {
		t.Error("expected empty horizon error")
	}
	if err := (services.RisksReply{Risks: []services.SuggestedRisk{{Title: "Риск", Horizon: "1 год", BaseProbabilityPercent: f(150)}}}).Validate(); err == nil {
		t.Error("expected bad base probability error")
	}
}

func TestCaseFromRisk(t *testing.T) {
	risk := services.SuggestedRisk{
		Title:                  "Новый риск",
		Category:               "деньги",
		Horizon:                "2 года",
		BaseProbabilityPercent: f(25),
		BaseSource:             "оценка",
		Actions:                []services.RiskAction{{Title: "Действие", Weight: f(-10), Confidence: "high", Reasoning: "почему"}},
	}
	c := services.CaseFromRisk(risk, "2026-11-17")
	if c.Kind != models.KindRisk || c.Title != "Новый риск" || c.BaseProbability != 0.25 {
		t.Fatalf("unexpected case: %+v", c)
	}
	if len(c.Actions) != 1 || c.Actions[0].LLMWeight == nil || *c.Actions[0].LLMWeight != -10 {
		t.Fatalf("action not mapped: %+v", c.Actions)
	}
	if c.Actions[0].Weight != nil {
		t.Error("weight should stay in llm_weight only")
	}
}

func TestHasCaseTitled(t *testing.T) {
	data := &models.Data{Cases: []models.Case{{Title: "Риск"}}}
	if !services.HasCaseTitled(data, " риск ") {
		t.Error("expected case-insensitive, trimmed match")
	}
	if services.HasCaseTitled(data, "Другое") {
		t.Error("unexpected match")
	}
}
