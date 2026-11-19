package services

import (
	"errors"
	"fmt"
	"strings"
	texttemplate "text/template"

	"gotovnost/internal/models"
)

// SuggestedRiskCount is how many main risks the neural net is asked for.
const SuggestedRiskCount = 6

// ---- request (what we send to the neural net) ----

type existingCaseSummary struct {
	Title    string      `json:"title"`
	Kind     models.Kind `json:"kind"`
	Category string      `json:"category"`
}

type risksRequest struct {
	Task          string                `json:"task"`
	Count         int                   `json:"count"`
	Focus         string                `json:"focus"`
	Profile       models.Profile        `json:"profile"`
	ExistingCases []existingCaseSummary `json:"existing_cases"`
}

// BuildRisksPrompt sends the profile and the existing cases, so the neural net does not repeat them.
// focus is the user's optional clarification of what to concentrate on; it is not stored.
func BuildRisksPrompt(promptTemplate *texttemplate.Template, data *models.Data, focus string) (string, error) {
	request := risksRequest{
		Task:          "suggest_main_risks",
		Count:         SuggestedRiskCount,
		Focus:         strings.TrimSpace(focus),
		Profile:       data.Profile,
		ExistingCases: []existingCaseSummary{},
	}
	for _, existingCase := range data.Cases {
		request.ExistingCases = append(request.ExistingCases, existingCaseSummary{
			Title: existingCase.Title, Kind: existingCase.Kind, Category: existingCase.Category,
		})
	}
	return renderPrompt(promptTemplate, request, false)
}

// ---- reply (what the neural net sends back) ----

type RiskAction struct {
	Title      string   `json:"title"`
	Weight     *float64 `json:"weight"`
	Confidence string   `json:"confidence"`
	Reasoning  string   `json:"reasoning"`
	Sources    []string `json:"sources"`
}

type SuggestedRisk struct {
	Title                  string       `json:"title"`
	Category               string       `json:"category"`
	Horizon                string       `json:"horizon"`
	BaseProbabilityPercent *float64     `json:"base_probability_percent"`
	BaseSource             string       `json:"base_source"`
	Description            string       `json:"description"`
	Actions                []RiskAction `json:"actions"`
}

type RisksReply struct {
	Risks []SuggestedRisk `json:"risks"`
}

// ParseRisksReply decodes and validates the risks reply.
func ParseRisksReply(rawReply string) (RisksReply, error) {
	var reply RisksReply
	if err := decodeReply(rawReply, &reply); err != nil {
		return RisksReply{}, err
	}
	return reply, reply.Validate()
}

func (reply RisksReply) Validate() error {
	if len(reply.Risks) == 0 {
		return errors.New("в ответе нет ни одного риска")
	}
	for riskIndex, risk := range reply.Risks {
		position := riskIndex + 1
		if strings.TrimSpace(risk.Title) == "" {
			return fmt.Errorf("риск №%d: пустое название", position)
		}
		if strings.TrimSpace(risk.Horizon) == "" {
			return fmt.Errorf("риск №%d: пустой horizon", position)
		}
		if risk.BaseProbabilityPercent == nil || *risk.BaseProbabilityPercent < 0 || *risk.BaseProbabilityPercent > 100 {
			return fmt.Errorf("риск №%d: base_probability_percent должен быть числом от 0 до 100", position)
		}
		for actionIndex, action := range risk.Actions {
			if strings.TrimSpace(action.Title) == "" {
				return fmt.Errorf("риск №%d, действие №%d: пустое название", position, actionIndex+1)
			}
			if !models.ValidWeight(models.KindRisk, action.Weight) {
				return fmt.Errorf("риск №%d, действие №%d: %w", position, actionIndex+1, models.WeightError(models.KindRisk))
			}
			if !ValidConfidence(action.Confidence) {
				return fmt.Errorf("риск №%d, действие №%d: confidence должен быть low, medium или high", position, actionIndex+1)
			}
		}
	}
	return nil
}

// CaseFromRisk turns a suggested risk into a new case. The neural net's weights go into
// llm_weight only, so nothing counts until the user accepts it, as with any other estimate.
func CaseFromRisk(risk SuggestedRisk, estimatedAt string) models.Case {
	newCase := models.Case{
		ID:              models.NewID("c"),
		Kind:            models.KindRisk,
		Category:        strings.TrimSpace(risk.Category),
		Title:           strings.TrimSpace(risk.Title),
		Description:     strings.TrimSpace(risk.Description),
		Horizon:         strings.TrimSpace(risk.Horizon),
		BaseProbability: *risk.BaseProbabilityPercent / 100,
		BaseSource:      strings.TrimSpace(risk.BaseSource),
		Actions:         []models.Action{},
	}
	for _, riskAction := range risk.Actions {
		newCase.Actions = append(newCase.Actions, models.Action{
			ID:          models.NewID("a"),
			Title:       strings.TrimSpace(riskAction.Title),
			LLMWeight:   riskAction.Weight,
			Confidence:  riskAction.Confidence,
			Reasoning:   riskAction.Reasoning,
			Sources:     riskAction.Sources,
			EstimatedAt: estimatedAt,
		})
	}
	return newCase
}

// HasCaseTitled reports whether a case with the given title already exists (case-insensitive).
func HasCaseTitled(data *models.Data, title string) bool {
	for _, existingCase := range data.Cases {
		if strings.EqualFold(strings.TrimSpace(existingCase.Title), strings.TrimSpace(title)) {
			return true
		}
	}
	return false
}
