package services_test

import (
	"strings"
	"testing"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

func refineFixture() (models.Case, models.Action) {
	c := models.Case{
		ID: "c1", Kind: models.KindAspiration, Title: "Цель", Category: "работа",
		Description: "Описание", Horizon: "1 год",
		Actions: []models.Action{{
			ID:    "a1",
			Title: "Действие",
			Steps: []models.Step{
				{ID: "s1", Title: "Первый"},
				{ID: "s2", Title: "Второй"},
				{ID: "s3", Title: "Третий"},
			},
		}},
	}
	return c, c.Actions[0]
}

func TestBuildRefinePrompt(t *testing.T) {
	prompts := promptTemplates(t)
	c, action := refineFixture()
	got, err := services.BuildRefinePrompt(prompts.Refine, c, action, 1)
	if err != nil {
		t.Fatalf("BuildRefinePrompt: %v", err)
	}
	if !strings.Contains(got, "refine_step") {
		t.Error("prompt should mention the task")
	}
	if !strings.Contains(got, `"action_id": "a1"`) {
		t.Error("prompt should include action_id")
	}
	if !strings.Contains(got, `"target_step"`) {
		t.Error("prompt should include target_step")
	}
	if !strings.Contains(got, `"id": "s2"`) {
		t.Error("prompt should reference the selected step id")
	}
	if !strings.Contains(got, `"number": 2`) {
		t.Error("prompt should number the target step")
	}
}

func TestParseRefineReply(t *testing.T) {
	raw := `{"action_id":"a1","step_id":"s2","steps":["Один","Два"]}`
	reply, err := services.ParseRefineReply(raw)
	if err != nil {
		t.Fatalf("ParseRefineReply: %v", err)
	}
	if reply.ActionID != "a1" || reply.StepID != "s2" || len(reply.Steps) != 2 {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}

func TestRefineReplyValidate(t *testing.T) {
	c, _ := refineFixture()

	valid := services.RefineReply{ActionID: "a1", StepID: "s2", Steps: []string{"Один", "Два"}}
	if err := valid.Validate(c, "a1", "s2"); err != nil {
		t.Fatalf("valid reply rejected: %v", err)
	}

	if err := (services.RefineReply{ActionID: "other", StepID: "s2", Steps: []string{"Один"}}).Validate(c, "a1", "s2"); err == nil {
		t.Error("expected action_id mismatch error")
	}
	if err := (services.RefineReply{ActionID: "a1", StepID: "other", Steps: []string{"Один"}}).Validate(c, "a1", "s2"); err == nil {
		t.Error("expected step_id mismatch error")
	}
	if err := (services.RefineReply{ActionID: "a1", StepID: "s2", Steps: []string{}}).Validate(c, "a1", "s2"); err == nil {
		t.Error("expected empty steps error")
	}
	if err := (services.RefineReply{ActionID: "a1", StepID: "s2", Steps: []string{" ", "Один"}}).Validate(c, "a1", "s2"); err == nil {
		t.Error("expected empty title error")
	}
	if err := (services.RefineReply{ActionID: "nope", StepID: "s2", Steps: []string{"Один"}}).Validate(c, "nope", "s2"); err == nil {
		t.Error("expected unknown action error")
	}
}
