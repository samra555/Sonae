package services

import (
	"errors"
	"fmt"
	"strings"
	texttemplate "text/template"

	"gotovnost/internal/models"
)

type refineCase struct {
	Title       string `json:"title"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Horizon     string `json:"horizon"`
}

type refineStep struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type refineRequest struct {
	Task       string       `json:"task"`
	Case       refineCase   `json:"case"`
	ActionID   string       `json:"action_id"`
	Action     string       `json:"action"`
	Steps      []refineStep `json:"steps"`
	TargetStep refineStep   `json:"target_step"`
}

// RefineReply is the neural net's answer for splitting a step. Its top-level schema is disjoint
// from AssistantReply (estimate weights): action_id/step_id/steps here vs
// case_id/actions/suggested_actions/comment there, so neither answer parses into the other under
// decodeReply's DisallowUnknownFields.
type RefineReply struct {
	ActionID string   `json:"action_id"`
	StepID   string   `json:"step_id"`
	Steps    []string `json:"steps"`
}

// ParseRefineReply decodes the refinement reply.
func ParseRefineReply(rawReply string) (RefineReply, error) {
	var reply RefineReply
	err := decodeReply(rawReply, &reply)
	return reply, err
}

// Validate checks the reply against the selected action and step and the case they live in.
func (reply RefineReply) Validate(targetCase models.Case, actionID, stepID string) error {
	if reply.ActionID != actionID {
		return fmt.Errorf("action_id %q не совпадает с выбранным %q", reply.ActionID, actionID)
	}
	if reply.StepID != stepID {
		return fmt.Errorf("step_id %q не совпадает с выбранным %q", reply.StepID, stepID)
	}

	var action *models.Action
	for actionIndex := range targetCase.Actions {
		if targetCase.Actions[actionIndex].ID == actionID {
			action = &targetCase.Actions[actionIndex]
			break
		}
	}
	if action == nil {
		return fmt.Errorf("действие %q не найдено", actionID)
	}
	if !actionHasStep(action, stepID) {
		return fmt.Errorf("шаг %q не найден", stepID)
	}

	if len(reply.Steps) == 0 {
		return errors.New("ответ не содержит под-шагов")
	}
	for _, title := range reply.Steps {
		if strings.TrimSpace(title) == "" {
			return errors.New("в ответе есть под-шаг с пустым названием")
		}
	}
	return nil
}

func actionHasStep(action *models.Action, stepID string) bool {
	for stepIndex := range action.Steps {
		if action.Steps[stepIndex].ID == stepID {
			return true
		}
	}
	return false
}

// BuildRefinePrompt assembles the prompt that asks the neural net to split a step.
func BuildRefinePrompt(promptTemplate *texttemplate.Template, targetCase models.Case, action models.Action, stepIndex int) (string, error) {
	steps := make([]refineStep, 0, len(action.Steps))
	for i := range action.Steps {
		step := action.Steps[i]
		steps = append(steps, refineStep{ID: step.ID, Number: i + 1, Title: step.Title})
	}
	request := refineRequest{
		Task: "refine_step",
		Case: refineCase{
			Title:       targetCase.Title,
			Category:    targetCase.Category,
			Description: targetCase.Description,
			Horizon:     targetCase.Horizon,
		},
		ActionID:   action.ID,
		Action:     action.Title,
		Steps:      steps,
		TargetStep: steps[stepIndex],
	}
	return renderPrompt(promptTemplate, request, false)
}
