package transport

import (
	"cmp"
	"math"
	"slices"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

type IndexRow struct {
	Kind                         models.Kind
	ID, Title, Category, Horizon string
	Base, Current                float64 // risks
	Completion                   float64 // aspirations
	DoneCount, TotalCount        int
	ProgressPercent              int
}

type IndexView struct {
	Login              string
	Profile            models.Profile
	Risks, Aspirations []IndexRow
}

type LinkOption struct {
	ID, Title string
}

type LinkGroup struct {
	Title   string
	Actions []LinkOption
}

type ActionView struct {
	models.Action
	Blocked         bool
	BlockReason     string
	CanAccept       bool
	OverlapTitles   []string
	RequiresChips   []LinkOption
	ExcludesChips   []LinkOption
	RequiresOptions []LinkGroup
	ExcludesOptions []LinkGroup
}

type CaseView struct {
	Login      string
	Case       models.Case
	Current    float64 // risks
	Completion float64 // aspirations
	// WeightSum and WeightSumOff: an aspiration's weights should add up to 100.
	WeightSum    float64
	WeightSumOff bool
	CanAcceptAny bool
	Actions      []ActionView
	Notice       string
	Profile      models.Profile
}

type PreviewRow struct {
	ActionID   string
	Title      string
	MyWeight   *float64
	Estimate   services.ActionEstimate
	Applicable bool
}

type SuggestedRow struct {
	Index int
	services.SuggestedAction
}

type ImportPreview struct {
	CaseID    string
	Kind      models.Kind
	RawReply  string
	Error     string
	Rows      []PreviewRow
	Suggested []SuggestedRow
	Comment   string
}

type RefinePreview struct {
	CaseID, ActionID, StepID string
	RawReply                 string
	Error                    string
	Steps                    []string
}

func linkChips(data *models.Data, linkedIDs []string) []LinkOption {
	chips := make([]LinkOption, 0, len(linkedIDs))
	for _, linkedID := range linkedIDs {
		chips = append(chips, LinkOption{ID: linkedID, Title: titleOf(data, linkedID)})
	}
	return chips
}

func titleOf(data *models.Data, actionID string) string {
	action := findAction(data, actionID)
	if action == nil {
		return actionID
	}
	return action.Title
}

func findAction(data *models.Data, actionID string) *models.Action {
	for caseIndex := range data.Cases {
		for actionIndex := range data.Cases[caseIndex].Actions {
			if data.Cases[caseIndex].Actions[actionIndex].ID == actionID {
				return &data.Cases[caseIndex].Actions[actionIndex]
			}
		}
	}
	return nil
}

// linkGroups lists every action that can still be linked, the current case first, then the others by title.
func linkGroups(data *models.Data, currentCaseID, selfID string, alreadyLinked []string) []LinkGroup {
	var groups []LinkGroup
	appendGroup := func(groupTitle string, sourceCase models.Case) {
		group := LinkGroup{Title: groupTitle}
		for _, candidate := range sourceCase.Actions {
			if candidate.ID == selfID || slices.Contains(alreadyLinked, candidate.ID) {
				continue
			}
			group.Actions = append(group.Actions, LinkOption{ID: candidate.ID, Title: candidate.Title})
		}
		if len(group.Actions) > 0 {
			groups = append(groups, group)
		}
	}

	if currentCase := data.FindCase(currentCaseID); currentCase != nil {
		appendGroup("Этот кейс", *currentCase)
	}
	for _, otherCase := range data.Cases {
		if otherCase.ID != currentCaseID {
			appendGroup(otherCase.Title, otherCase)
		}
	}
	return groups
}

// BuildCaseView assembles the case page's view model.
func BuildCaseView(data *models.Data, caseID, notice string) (CaseView, bool) {
	foundCase := data.FindCase(caseID)
	if foundCase == nil {
		return CaseView{}, false
	}

	caseView := CaseView{
		Case:       *foundCase,
		Current:    foundCase.CurrentProbability(),
		Completion: foundCase.Completion(),
		WeightSum:  foundCase.WeightSum(),
		Actions:    make([]ActionView, 0, len(foundCase.Actions)),
		Notice:     notice,
		Profile:    data.Profile,
	}
	caseView.WeightSumOff = foundCase.IsAspiration() && len(foundCase.Actions) > 0 &&
		math.Abs(caseView.WeightSum-100) > 0.001

	for _, action := range foundCase.Actions {
		actionView := ActionView{
			Action:          action,
			CanAccept:       action.LLMWeight != nil && !models.SameNumber(action.LLMWeight, action.Weight),
			RequiresChips:   linkChips(data, action.Requires),
			ExcludesChips:   linkChips(data, action.Excludes),
			RequiresOptions: linkGroups(data, foundCase.ID, action.ID, action.Requires),
			ExcludesOptions: linkGroups(data, foundCase.ID, action.ID, action.Excludes),
		}
		caseView.CanAcceptAny = caseView.CanAcceptAny || actionView.CanAccept

		if !action.Done {
			actionView.BlockReason = data.BlockReason(&action)
			actionView.Blocked = actionView.BlockReason != ""
		}
		for _, overlapID := range action.OverlapsWith {
			actionView.OverlapTitles = append(actionView.OverlapTitles, titleOf(data, overlapID))
		}
		caseView.Actions = append(caseView.Actions, actionView)
	}

	benefit := func(weight float64) float64 {
		if foundCase.IsAspiration() {
			return weight
		}
		return -weight
	}
	slices.SortStableFunc(caseView.Actions, func(first, second ActionView) int {
		if (first.Weight == nil) != (second.Weight == nil) {
			if first.Weight == nil {
				return 1
			}
			return -1
		}
		if first.Weight == nil {
			return 0
		}
		return cmp.Compare(benefit(*second.Weight), benefit(*first.Weight))
	})

	return caseView, true
}

// BuildPreviewRows maps the reply's estimates to the case's actions for the preview table.
func BuildPreviewRows(targetCase models.Case, reply services.AssistantReply) []PreviewRow {
	actionsByID := map[string]models.Action{}
	for _, action := range targetCase.Actions {
		actionsByID[action.ID] = action
	}

	rows := make([]PreviewRow, 0, len(reply.Actions))
	for _, estimate := range reply.Actions {
		action := actionsByID[estimate.ID]
		rows = append(rows, PreviewRow{
			ActionID:   action.ID,
			Title:      action.Title,
			MyWeight:   action.Weight,
			Estimate:   estimate,
			Applicable: estimate.Weight != nil,
		})
	}
	return rows
}
