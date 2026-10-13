package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

type Kind string

const (
	KindRisk       Kind = "risk"
	KindAspiration Kind = "aspiration"
)

const (
	minMultiplier = 0.01
	maxMultiplier = 5.0
)

type Profile struct {
	Age           int    `json:"age"`
	Sex           string `json:"sex,omitempty"`
	Country       string `json:"country"`
	City          string `json:"city,omitempty"`
	Profession    string `json:"profession,omitempty"`
	MaritalStatus string `json:"marital_status,omitempty"`
	Children      string `json:"children,omitempty"`
	// Free text: chronic conditions, medications, allergies and the like.
	Health string `json:"health,omitempty"`
	// Free text: anything else the user wants the neural net to know.
	About string `json:"about,omitempty"`
}

// Allowed values of the profile dropdowns; "" means "not specified".
var (
	sexOptions           = []string{"", "male", "female"}
	maritalStatusOptions = []string{"", "single", "in_relationship", "married", "divorced", "widowed"}
	childrenOptions      = []string{"", "no", "yes"}
)

// Step is one item of an action's checklist. Steps do not affect the probability:
// only the action does, and it is marked done automatically once every step is done.
type Step struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type Action struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Done     bool     `json:"done"`
	Requires []string `json:"requires,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	Steps    []Step   `json:"steps,omitempty"`

	// MyMultiplier is the only number used in the probability calculation.
	MyMultiplier *float64 `json:"my_multiplier,omitempty"`

	// The neural net's estimate is stored next to it, but never counted until the user accepts it.
	LLMMultiplier *float64 `json:"llm_multiplier,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Reasoning     string   `json:"reasoning,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	OverlapsWith  []string `json:"overlaps_with,omitempty"`
	EstimatedAt   string   `json:"estimated_at,omitempty"`
}

type Case struct {
	ID              string   `json:"id"`
	Kind            Kind     `json:"kind"`
	Category        string   `json:"category"`
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	Horizon         string   `json:"horizon"`
	BaseProbability float64  `json:"base_probability"`
	BaseSource      string   `json:"base_source"`
	Actions         []Action `json:"actions"`
}

type Data struct {
	Profile Profile `json:"profile"`
	Cases   []Case  `json:"cases"`
}

var (
	errCaseNotFound   = errors.New("кейс не найден")
	errActionNotFound = errors.New("действие не найдено")
	errStepNotFound   = errors.New("шаг не найден")
)

// CurrentProbability multiplies the base probability by the multipliers of done actions.
// Multipliers (not percentage-point deltas) are used so the result stays meaningful
// no matter how many actions are combined.
func (c Case) CurrentProbability() float64 {
	probability := c.BaseProbability
	for _, action := range c.Actions {
		if !action.Done || action.MyMultiplier == nil {
			continue
		}
		probability *= *action.MyMultiplier
	}

	maxProbability := 1.0
	if c.Kind == KindAspiration {
		maxProbability = 0.99
	}
	return math.Min(math.Max(probability, 0), maxProbability)
}

func (d *Data) findCase(caseID string) *Case {
	for caseIndex := range d.Cases {
		if d.Cases[caseIndex].ID == caseID {
			return &d.Cases[caseIndex]
		}
	}
	return nil
}

func (d *Data) findAction(actionID string) *Action {
	for caseIndex := range d.Cases {
		for actionIndex := range d.Cases[caseIndex].Actions {
			if d.Cases[caseIndex].Actions[actionIndex].ID == actionID {
				return &d.Cases[caseIndex].Actions[actionIndex]
			}
		}
	}
	return nil
}

func (d *Data) allActions() []*Action {
	var actions []*Action
	for caseIndex := range d.Cases {
		for actionIndex := range d.Cases[caseIndex].Actions {
			actions = append(actions, &d.Cases[caseIndex].Actions[actionIndex])
		}
	}
	return actions
}

func (d *Data) titleOf(actionID string) string {
	action := d.findAction(actionID)
	if action == nil {
		return actionID
	}
	return action.Title
}

// BlockReason returns why the action cannot be marked done now, or "" if it can.
func (d *Data) BlockReason(target *Action) string {
	for _, requiredID := range target.Requires {
		required := d.findAction(requiredID)
		if required != nil && required.Done {
			continue
		}
		return "сначала: " + d.titleOf(requiredID)
	}

	for _, other := range d.allActions() {
		if !other.Done {
			continue
		}
		if slices.Contains(other.Excludes, target.ID) || slices.Contains(target.Excludes, other.ID) {
			return "несовместимо с: " + other.Title
		}
	}
	return ""
}

func (d *Data) doneDependentOn(target *Action) *Action {
	for _, other := range d.allActions() {
		if other.Done && slices.Contains(other.Requires, target.ID) {
			return other
		}
	}
	return nil
}

func (d *Data) ToggleAction(action *Action) error {
	if action.Done {
		if dependent := d.doneDependentOn(action); dependent != nil {
			return fmt.Errorf("нельзя снять: от этого зависит «%s»", dependent.Title)
		}
		action.Done = false
		return nil
	}

	if reason := d.BlockReason(action); reason != "" {
		return errors.New("действие заблокировано, " + reason)
	}
	action.Done = true
	return nil
}

func (a *Action) findStep(stepID string) *Step {
	for stepIndex := range a.Steps {
		if a.Steps[stepIndex].ID == stepID {
			return &a.Steps[stepIndex]
		}
	}
	return nil
}

func (a Action) DoneStepCount() int {
	doneCount := 0
	for _, step := range a.Steps {
		if step.Done {
			doneCount++
		}
	}
	return doneCount
}

// ToggleStep flips a step and keeps the action in sync: the last step done marks the action done,
// and undoing a step of a done action un-marks it. If the action cannot follow, the step is refused too.
// An action marked done by hand while some steps are open stays done when other steps are checked.
func (d *Data) ToggleStep(action *Action, step *Step) error {
	step.Done = !step.Done
	completesAction := step.Done && !action.Done && action.DoneStepCount() == len(action.Steps)
	reopensAction := !step.Done && action.Done
	if !completesAction && !reopensAction {
		return nil
	}
	if err := d.ToggleAction(action); err != nil {
		return fmt.Errorf("шаг не отмечен: %w", err)
	}
	return nil
}

func validMultiplier(multiplier *float64) bool {
	if multiplier == nil {
		return true
	}
	return *multiplier >= minMultiplier && *multiplier <= maxMultiplier
}

func validConfidence(confidence string) bool {
	return confidence == "low" || confidence == "medium" || confidence == "high"
}

// Validate checks the invariants the rest of the code relies on.
func (d *Data) Validate() error {
	if d.Profile.Age < 0 || d.Profile.Age > 120 {
		return fmt.Errorf("возраст должен быть от 0 до 120")
	}
	if !slices.Contains(sexOptions, d.Profile.Sex) ||
		!slices.Contains(maritalStatusOptions, d.Profile.MaritalStatus) ||
		!slices.Contains(childrenOptions, d.Profile.Children) {
		return fmt.Errorf("профиль: недопустимое значение в списке")
	}

	caseIDs := map[string]bool{}
	actionsByID := map[string]*Action{}

	for caseIndex := range d.Cases {
		currentCase := &d.Cases[caseIndex]
		if currentCase.ID == "" || caseIDs[currentCase.ID] {
			return fmt.Errorf("пустой или повторяющийся id кейса: %q", currentCase.ID)
		}
		caseIDs[currentCase.ID] = true

		if strings.TrimSpace(currentCase.Title) == "" {
			return fmt.Errorf("кейс %q: пустое название", currentCase.ID)
		}
		if currentCase.Kind != KindRisk && currentCase.Kind != KindAspiration {
			return fmt.Errorf("кейс %q: kind должен быть risk или aspiration", currentCase.ID)
		}
		if currentCase.BaseProbability < 0 || currentCase.BaseProbability > 1 {
			return fmt.Errorf("кейс %q: base_probability должна быть от 0 до 1", currentCase.ID)
		}

		for actionIndex := range currentCase.Actions {
			action := &currentCase.Actions[actionIndex]
			if action.ID == "" || actionsByID[action.ID] != nil {
				return fmt.Errorf("пустой или повторяющийся id действия: %q", action.ID)
			}
			if strings.TrimSpace(action.Title) == "" {
				return fmt.Errorf("действие %q: пустое название", action.ID)
			}
			if !validMultiplier(action.LLMMultiplier) || !validMultiplier(action.MyMultiplier) {
				return fmt.Errorf("действие %q: множитель вне диапазона %.2f–%.0f", action.ID, minMultiplier, maxMultiplier)
			}
			stepIDs := map[string]bool{}
			for _, step := range action.Steps {
				if step.ID == "" || stepIDs[step.ID] {
					return fmt.Errorf("действие %q: пустой или повторяющийся id шага %q", action.ID, step.ID)
				}
				stepIDs[step.ID] = true
				if strings.TrimSpace(step.Title) == "" {
					return fmt.Errorf("действие %q: пустое название шага", action.ID)
				}
			}
			actionsByID[action.ID] = action
		}
	}

	for _, action := range actionsByID {
		for _, linkedID := range slices.Concat(action.Requires, action.Excludes) {
			if actionsByID[linkedID] == nil {
				return fmt.Errorf("действие %q ссылается на неизвестное действие %q", action.ID, linkedID)
			}
		}
	}
	return checkRequiresCycles(actionsByID)
}

func checkRequiresCycles(actionsByID map[string]*Action) error {
	const (
		visiting = iota + 1
		visited
	)
	visitState := map[string]int{}

	var visit func(actionID string) error
	visit = func(actionID string) error {
		switch visitState[actionID] {
		case visiting:
			return fmt.Errorf("циклическая зависимость в «Сначала нужно выполнить»: «%s»", actionsByID[actionID].Title)
		case visited:
			return nil
		}

		visitState[actionID] = visiting
		for _, requiredID := range actionsByID[actionID].Requires {
			if err := visit(requiredID); err != nil {
				return err
			}
		}
		visitState[actionID] = visited
		return nil
	}

	for actionID := range actionsByID {
		if err := visit(actionID); err != nil {
			return err
		}
	}
	return nil
}

func (c Case) DoneCount() int {
	doneCount := 0
	for _, action := range c.Actions {
		if action.Done {
			doneCount++
		}
	}
	return doneCount
}

func (c Case) ProgressPercent() int {
	if len(c.Actions) == 0 {
		return 0
	}
	return c.DoneCount() * 100 / len(c.Actions)
}

// DeltaPoints is the change from the base probability in percentage points.
func (c Case) DeltaPoints() float64 {
	return (c.CurrentProbability() - c.BaseProbability) * 100
}

// Improved says whether the change is in the desired direction:
// lower for a risk, higher for an aspiration.
func (c Case) Improved() bool {
	if c.Kind == KindRisk {
		return c.DeltaPoints() < 0
	}
	return c.DeltaPoints() > 0
}
