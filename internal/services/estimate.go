package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	texttemplate "text/template"

	"gotovnost/internal/models"
)

// ---- shared helpers ----

func validConfidence(confidence string) bool {
	return confidence == "low" || confidence == "medium" || confidence == "high"
}

// ValidConfidence reports whether the confidence value is allowed.
func ValidConfidence(confidence string) bool {
	return validConfidence(confidence)
}

// decodeReply takes everything between the first "{" and the last "}", so it
// survives ```json fences and chatter around the JSON, then decodes it strictly.
func decodeReply(rawReply string, target any) error {
	jsonStart := strings.Index(rawReply, "{")
	jsonEnd := strings.LastIndex(rawReply, "}")
	if jsonStart < 0 || jsonEnd < jsonStart {
		return errors.New("в ответе не найден JSON")
	}

	decoder := json.NewDecoder(strings.NewReader(rawReply[jsonStart : jsonEnd+1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("ответ не соответствует схеме: %w", err)
	}
	return nil
}

// renderPrompt puts the request JSON into the template; the template sees .JSON and .Aspiration.
func renderPrompt(promptTemplate *texttemplate.Template, request any, aspiration bool) (string, error) {
	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return "", fmt.Errorf("не удалось собрать JSON: %w", err)
	}

	var promptBuffer bytes.Buffer
	templateData := struct {
		JSON       string
		Aspiration bool
	}{JSON: string(requestJSON), Aspiration: aspiration}
	if err := promptTemplate.Execute(&promptBuffer, templateData); err != nil {
		return "", fmt.Errorf("не удалось собрать промпт: %w", err)
	}
	return promptBuffer.String(), nil
}

// StepKey builds the form key for a suggested step of an action.
func StepKey(actionID string, stepIndex int) string {
	return fmt.Sprintf("%s/%d", actionID, stepIndex)
}

// AddSteps appends steps to the action's checklist, skipping titles it already has.
func AddSteps(action *models.Action, stepTitles []string) {
	for _, stepTitle := range stepTitles {
		stepTitle = strings.TrimSpace(stepTitle)
		alreadyThere := slices.ContainsFunc(action.Steps, func(step models.Step) bool {
			return strings.EqualFold(step.Title, stepTitle)
		})
		if !alreadyThere {
			action.Steps = append(action.Steps, models.Step{ID: models.NewID("s"), Title: stepTitle})
		}
	}
}

// ---- request (what we send to the neural net) ----

type exportCase struct {
	ID          string      `json:"id"`
	Kind        models.Kind `json:"kind"`
	Category    string      `json:"category"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Horizon     string      `json:"horizon"`
	// Risks only: an aspiration has no base probability.
	BaseProbabilityPercent *float64 `json:"base_probability_percent,omitempty"`
	BaseSource             string   `json:"base_source,omitempty"`
}

type exportStep struct {
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type exportAction struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Done     bool         `json:"done"`
	Steps    []exportStep `json:"steps"`
	Requires []string     `json:"requires"`
	Excludes []string     `json:"excludes"`
	MyWeight *float64     `json:"my_weight"`
}

type estimateRequest struct {
	Task    string         `json:"task"`
	Profile models.Profile `json:"profile"`
	Case    exportCase     `json:"case"`
	Actions []exportAction `json:"actions"`
}

func orEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// BuildPrompt assembles the estimate prompt that asks the neural net for action weights.
func BuildPrompt(promptTemplate *texttemplate.Template, profile models.Profile, targetCase models.Case) (string, error) {
	request := estimateRequest{
		Task:    "estimate_action_weights",
		Profile: profile,
		Case: exportCase{
			ID:          targetCase.ID,
			Kind:        targetCase.Kind,
			Category:    targetCase.Category,
			Title:       targetCase.Title,
			Description: targetCase.Description,
			Horizon:     targetCase.Horizon,
		},
		Actions: []exportAction{},
	}
	if !targetCase.IsAspiration() {
		basePercent := math.Round(targetCase.BaseProbability*100*10000) / 10000
		request.Case.BaseProbabilityPercent = &basePercent
		request.Case.BaseSource = targetCase.BaseSource
	}
	for _, action := range targetCase.Actions {
		steps := []exportStep{}
		for _, step := range action.Steps {
			steps = append(steps, exportStep{Title: step.Title, Done: step.Done})
		}
		request.Actions = append(request.Actions, exportAction{
			ID:       action.ID,
			Title:    action.Title,
			Done:     action.Done,
			Steps:    steps,
			Requires: orEmpty(action.Requires),
			Excludes: orEmpty(action.Excludes),
			MyWeight: action.Weight,
		})
	}

	return renderPrompt(promptTemplate, request, targetCase.IsAspiration())
}

// ---- reply (what the neural net sends back) ----

type ActionEstimate struct {
	ID             string   `json:"id"`
	Weight         *float64 `json:"weight"`
	Confidence     string   `json:"confidence"`
	Reasoning      string   `json:"reasoning"`
	Sources        []string `json:"sources"`
	OverlapsWith   []string `json:"overlaps_with"`
	SuggestedSteps []string `json:"suggested_steps"`
}

type SuggestedAction struct {
	Title          string   `json:"title"`
	Weight         *float64 `json:"weight"`
	Confidence     string   `json:"confidence"`
	Reasoning      string   `json:"reasoning"`
	SuggestedSteps []string `json:"suggested_steps"`
}

type AssistantReply struct {
	CaseID           string            `json:"case_id"`
	Actions          []ActionEstimate  `json:"actions"`
	SuggestedActions []SuggestedAction `json:"suggested_actions"`
	Comment          string            `json:"comment"`
}

// ParseAssistantReply decodes and validates the estimate reply.
func ParseAssistantReply(rawReply string) (AssistantReply, error) {
	var reply AssistantReply
	err := decodeReply(rawReply, &reply)
	return reply, err
}

func (reply AssistantReply) Validate(targetCase models.Case) error {
	if reply.CaseID != targetCase.ID {
		return fmt.Errorf("case_id %q не совпадает с текущим кейсом %q", reply.CaseID, targetCase.ID)
	}

	knownActionIDs := map[string]bool{}
	for _, action := range targetCase.Actions {
		knownActionIDs[action.ID] = true
	}

	seenActionIDs := map[string]bool{}
	for _, estimate := range reply.Actions {
		if !knownActionIDs[estimate.ID] {
			return fmt.Errorf("в ответе неизвестное действие %q", estimate.ID)
		}
		if seenActionIDs[estimate.ID] {
			return fmt.Errorf("действие %q встречается в ответе дважды", estimate.ID)
		}
		seenActionIDs[estimate.ID] = true

		if !models.ValidWeight(targetCase.Kind, estimate.Weight) {
			return fmt.Errorf("действие %q: %w", estimate.ID, models.WeightError(targetCase.Kind))
		}
		if !validConfidence(estimate.Confidence) {
			return fmt.Errorf("действие %q: confidence должен быть low, medium или high", estimate.ID)
		}
		for _, stepTitle := range estimate.SuggestedSteps {
			if strings.TrimSpace(stepTitle) == "" {
				return fmt.Errorf("действие %q: пустой шаг в suggested_steps", estimate.ID)
			}
		}
	}

	for suggestedIndex, suggested := range reply.SuggestedActions {
		position := suggestedIndex + 1
		if strings.TrimSpace(suggested.Title) == "" {
			return fmt.Errorf("предложенное действие №%d: пустое название", position)
		}
		if !models.ValidWeight(targetCase.Kind, suggested.Weight) {
			return fmt.Errorf("предложенное действие №%d: %w", position, models.WeightError(targetCase.Kind))
		}
		if !validConfidence(suggested.Confidence) {
			return fmt.Errorf("предложенное действие №%d: неверный confidence", position)
		}
		for _, stepTitle := range suggested.SuggestedSteps {
			if strings.TrimSpace(stepTitle) == "" {
				return fmt.Errorf("предложенное действие №%d: пустой шаг в suggested_steps", position)
			}
		}
	}
	return nil
}

type ApplySelection struct {
	ActionIDs        map[string]bool
	SuggestedIndexes map[int]bool
	// StepKeys holds stepKey(actionID, index) of the suggested steps to add.
	StepKeys map[string]bool
}

// ApplyReply copies the accepted weights and steps from the reply into the case.
func ApplyReply(targetCase *models.Case, reply AssistantReply, selection ApplySelection, estimatedAt string) {
	actionsByID := map[string]*models.Action{}
	for actionIndex := range targetCase.Actions {
		actionsByID[targetCase.Actions[actionIndex].ID] = &targetCase.Actions[actionIndex]
	}

	for _, estimate := range reply.Actions {
		action := actionsByID[estimate.ID]

		var chosenSteps []string
		for stepIndex, stepTitle := range estimate.SuggestedSteps {
			if selection.StepKeys[StepKey(estimate.ID, stepIndex)] {
				chosenSteps = append(chosenSteps, stepTitle)
			}
		}
		AddSteps(action, chosenSteps)

		if !selection.ActionIDs[estimate.ID] || estimate.Weight == nil {
			continue
		}
		action.LLMWeight = estimate.Weight
		action.Confidence = estimate.Confidence
		action.Reasoning = estimate.Reasoning
		action.Sources = estimate.Sources
		action.OverlapsWith = estimate.OverlapsWith
		action.EstimatedAt = estimatedAt
	}

	for suggestedIndex, suggested := range reply.SuggestedActions {
		if !selection.SuggestedIndexes[suggestedIndex] {
			continue
		}
		newAction := models.Action{
			ID:          models.NewID("a"),
			Title:       suggested.Title,
			LLMWeight:   suggested.Weight,
			Confidence:  suggested.Confidence,
			Reasoning:   suggested.Reasoning,
			EstimatedAt: estimatedAt,
		}
		AddSteps(&newAction, suggested.SuggestedSteps)
		targetCase.Actions = append(targetCase.Actions, newAction)
	}
}
