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
	Age     int    `json:"age"`
	Country string `json:"country"`
}

type Action struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Done     bool     `json:"done"`
	Requires []string `json:"requires,omitempty"`
	Excludes []string `json:"excludes,omitempty"`

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
