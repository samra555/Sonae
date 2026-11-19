package transport_test

import (
	"testing"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
	"gotovnost/internal/transport"
)

func f(v float64) *float64 { return &v }

func TestBuildCaseViewSortsByWeight(t *testing.T) {
	data := &models.Data{
		Profile: models.Profile{Age: 30, Country: "Россия"},
		Cases: []models.Case{{
			ID: "c1", Kind: models.KindAspiration, Title: "Цель", Horizon: "1 год",
			Actions: []models.Action{
				{ID: "a1", Title: "Без веса"},
				{ID: "a2", Title: "Вес 30", Weight: f(30)},
				{ID: "a3", Title: "Вес 60", Weight: f(60)},
			},
		}},
	}
	view, ok := transport.BuildCaseView(data, "c1", "")
	if !ok {
		t.Fatal("case not found")
	}
	if len(view.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(view.Actions))
	}
	if view.Actions[0].Title != "Вес 60" || view.Actions[1].Title != "Вес 30" || view.Actions[2].Title != "Без веса" {
		t.Fatalf("wrong order: %s, %s, %s", view.Actions[0].Title, view.Actions[1].Title, view.Actions[2].Title)
	}
}

func TestBuildCaseViewWeightSumOff(t *testing.T) {
	data := &models.Data{
		Profile: models.Profile{Age: 30, Country: "Россия"},
		Cases: []models.Case{{
			ID: "c1", Kind: models.KindAspiration, Title: "Цель", Horizon: "1 год",
			Actions: []models.Action{{ID: "a1", Title: "Один", Weight: f(30)}},
		}},
	}
	view, _ := transport.BuildCaseView(data, "c1", "")
	if !view.WeightSumOff {
		t.Error("expected WeightSumOff when weights do not add to 100")
	}
}

func TestBuildCaseViewCanAccept(t *testing.T) {
	data := &models.Data{
		Profile: models.Profile{Age: 30, Country: "Россия"},
		Cases: []models.Case{{
			ID: "c1", Kind: models.KindAspiration, Title: "Цель", Horizon: "1 год",
			Actions: []models.Action{
				{ID: "a1", Title: "Один", Weight: f(10), LLMWeight: f(20)},
				{ID: "a2", Title: "Два", Weight: f(10), LLMWeight: f(10)},
			},
		}},
	}
	view, _ := transport.BuildCaseView(data, "c1", "")
	if !view.Actions[0].CanAccept {
		t.Error("action with different llm weight should be acceptable")
	}
	if view.Actions[1].CanAccept {
		t.Error("action with matching llm weight should not be acceptable")
	}
}

func TestBuildPreviewRows(t *testing.T) {
	targetCase := models.Case{ID: "c1", Kind: models.KindAspiration, Actions: []models.Action{
		{ID: "a1", Title: "Один", Weight: f(10)},
	}}
	reply := services.AssistantReply{
		CaseID:  "c1",
		Actions: []services.ActionEstimate{{ID: "a1", Weight: f(20), Confidence: "high"}},
	}
	rows := transport.BuildPreviewRows(targetCase, reply)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Title != "Один" || !rows[0].Applicable {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	if rows[0].MyWeight == nil || *rows[0].MyWeight != 10 {
		t.Fatalf("my weight not copied: %+v", rows[0])
	}
}
