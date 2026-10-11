package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	texttemplate "text/template"
)

// ---- request (what we send to the neural net) ----

type exportCase struct {
	ID              string  `json:"id"`
	Kind            Kind    `json:"kind"`
	Category        string  `json:"category"`
	Title           string  `json:"title"`
	Horizon         string  `json:"horizon"`
	BaseProbability float64 `json:"base_probability"`
	BaseSource      string  `json:"base_source"`
}

type exportAction struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Done            bool     `json:"done"`
	Requires        []string `json:"requires"`
	Excludes        []string `json:"excludes"`
	MyEffectPercent *float64 `json:"my_effect_percent"`
}

type estimateRequest struct {
	Task    string         `json:"task"`
	Profile Profile        `json:"profile"`
	Case    exportCase     `json:"case"`
	Actions []exportAction `json:"actions"`
}

func orEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func buildPrompt(promptTemplate *texttemplate.Template, profile Profile, targetCase Case) (string, error) {
	request := estimateRequest{
		Task:    "estimate_action_effects",
		Profile: profile,
		Case: exportCase{
			ID:              targetCase.ID,
			Kind:            targetCase.Kind,
			Category:        targetCase.Category,
			Title:           targetCase.Title,
			Horizon:         targetCase.Horizon,
			BaseProbability: targetCase.BaseProbability,
			BaseSource:      targetCase.BaseSource,
		},
		Actions: []exportAction{},
	}
	for _, action := range targetCase.Actions {
		request.Actions = append(request.Actions, exportAction{
			ID:              action.ID,
			Title:           action.Title,
			Done:            action.Done,
			Requires:        orEmpty(action.Requires),
			Excludes:        orEmpty(action.Excludes),
			MyEffectPercent: optionalPercent(action.MyMultiplier),
		})
	}

	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return "", fmt.Errorf("не удалось собрать JSON: %w", err)
	}

	var promptBuffer bytes.Buffer
	templateData := struct{ JSON string }{JSON: string(requestJSON)}
	if err := promptTemplate.Execute(&promptBuffer, templateData); err != nil {
		return "", fmt.Errorf("не удалось собрать промпт: %w", err)
	}
	return promptBuffer.String(), nil
}

// ---- reply (what the neural net sends back) ----

type ActionEstimate struct {
	ID            string   `json:"id"`
	EffectPercent *float64 `json:"effect_percent"`
	Confidence    string   `json:"confidence"`
	Reasoning     string   `json:"reasoning"`
	Sources       []string `json:"sources"`
	OverlapsWith  []string `json:"overlaps_with"`
}

type SuggestedAction struct {
	Title         string   `json:"title"`
	EffectPercent *float64 `json:"effect_percent"`
	Confidence    string   `json:"confidence"`
	Reasoning     string   `json:"reasoning"`
}

type AssistantReply struct {
	CaseID                 string            `json:"case_id"`
	Actions                []ActionEstimate  `json:"actions"`
	SuggestedActions       []SuggestedAction `json:"suggested_actions"`
	BaseProbabilityComment string            `json:"base_probability_comment"`
}

// parseAssistantReply takes everything between the first "{" and the last "}", so it
// survives ```json fences and chatter around the JSON, then decodes it strictly.
func parseAssistantReply(rawReply string) (AssistantReply, error) {
	jsonStart := strings.Index(rawReply, "{")
	jsonEnd := strings.LastIndex(rawReply, "}")
	if jsonStart < 0 || jsonEnd < jsonStart {
		return AssistantReply{}, errors.New("в ответе не найден JSON")
	}

	decoder := json.NewDecoder(strings.NewReader(rawReply[jsonStart : jsonEnd+1]))
	decoder.DisallowUnknownFields()

	var reply AssistantReply
	if err := decoder.Decode(&reply); err != nil {
		return AssistantReply{}, fmt.Errorf("ответ не соответствует схеме: %w", err)
	}
	return reply, nil
}

func (reply AssistantReply) Validate(targetCase Case) error {
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

		if !validEffectPercent(estimate.EffectPercent) {
			return fmt.Errorf("действие %q: effect_percent вне диапазона %.0f…%.0f", estimate.ID, minEffectPercent, maxEffectPercent)
		}
		if !validConfidence(estimate.Confidence) {
			return fmt.Errorf("действие %q: confidence должен быть low, medium или high", estimate.ID)
		}
	}

	for suggestedIndex, suggested := range reply.SuggestedActions {
		position := suggestedIndex + 1
		if strings.TrimSpace(suggested.Title) == "" {
			return fmt.Errorf("предложенное действие №%d: пустое название", position)
		}
		if !validEffectPercent(suggested.EffectPercent) {
			return fmt.Errorf("предложенное действие №%d: effect_percent вне диапазона", position)
		}
		if !validConfidence(suggested.Confidence) {
			return fmt.Errorf("предложенное действие №%d: неверный confidence", position)
		}
	}
	return nil
}

type ApplySelection struct {
	ActionIDs        map[string]bool
	SuggestedIndexes map[int]bool
}

func applyReply(targetCase *Case, reply AssistantReply, selection ApplySelection, estimatedAt string) {
	actionsByID := map[string]*Action{}
	for actionIndex := range targetCase.Actions {
		actionsByID[targetCase.Actions[actionIndex].ID] = &targetCase.Actions[actionIndex]
	}

	for _, estimate := range reply.Actions {
		if !selection.ActionIDs[estimate.ID] || estimate.EffectPercent == nil {
			continue
		}
		action := actionsByID[estimate.ID]
		action.LLMMultiplier = multiplierFromPercent(estimate.EffectPercent)
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
		targetCase.Actions = append(targetCase.Actions, Action{
			ID:            newID("a"),
			Title:         suggested.Title,
			LLMMultiplier: multiplierFromPercent(suggested.EffectPercent),
			Confidence:    suggested.Confidence,
			Reasoning:     suggested.Reasoning,
			EstimatedAt:   estimatedAt,
		})
	}
}
