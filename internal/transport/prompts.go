package transport

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	var promptText string
	var promptErr error
	caseFound := false
	storeOf(r).Read(func(data *models.Data) {
		targetCase := data.FindCase(r.PathValue("id"))
		if targetCase == nil {
			return
		}
		caseFound = true
		promptText, promptErr = services.BuildPrompt(s.prompts.Estimate, data.Profile, *targetCase)
	})

	if !caseFound {
		http.NotFound(w, r)
		return
	}
	if promptErr != nil {
		http.Error(w, promptErr.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "case", "prompt-box", struct{ Prompt string }{Prompt: promptText})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")

	var targetCase models.Case
	caseFound := false
	storeOf(r).Read(func(data *models.Data) {
		if foundCase := data.FindCase(caseID); foundCase != nil {
			targetCase, caseFound = *foundCase, true
		}
	})
	if !caseFound {
		http.NotFound(w, r)
		return
	}

	preview := ImportPreview{CaseID: caseID, Kind: targetCase.Kind, RawReply: r.FormValue("reply")}

	reply, err := services.ParseAssistantReply(preview.RawReply)
	if err == nil {
		err = reply.Validate(targetCase)
	}
	if err != nil {
		preview.Error = err.Error()
		s.render(w, "case", "import-preview", preview)
		return
	}

	preview.Rows = BuildPreviewRows(targetCase, reply)
	preview.Comment = reply.Comment
	for suggestedIndex, suggested := range reply.SuggestedActions {
		preview.Suggested = append(preview.Suggested, SuggestedRow{Index: suggestedIndex, SuggestedAction: suggested})
	}
	s.render(w, "case", "import-preview", preview)
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	selection := services.ApplySelection{ActionIDs: map[string]bool{}, SuggestedIndexes: map[int]bool{}, StepKeys: map[string]bool{}}
	for _, actionID := range r.Form["apply"] {
		selection.ActionIDs[actionID] = true
	}
	for _, chosenStepKey := range r.Form["step"] {
		selection.StepKeys[chosenStepKey] = true
	}
	for _, rawIndex := range r.Form["add"] {
		if suggestedIndex, err := strconv.Atoi(rawIndex); err == nil {
			selection.SuggestedIndexes[suggestedIndex] = true
		}
	}

	reply, err := services.ParseAssistantReply(r.FormValue("reply"))
	if err == nil {
		err = storeOf(r).Update(func(data *models.Data) error {
			targetCase := data.FindCase(caseID)
			if targetCase == nil {
				return models.ErrCaseNotFound
			}
			if err := reply.Validate(*targetCase); err != nil {
				return err
			}
			services.ApplyReply(targetCase, reply, selection, time.Now().Format("2006-01-02"))
			return nil
		})
	}

	notice := ""
	if err != nil {
		notice = err.Error()
	}
	if s.renderCaseBody(w, r, caseID, notice) {
		_, _ = io.WriteString(w, `<div id="import-result" hx-swap-oob="true"></div>`)
	}
}

// ---- step refinement ----

// handleRefinePrompt builds the prompt that asks the neural net to split the selected step.
func (s *Server) handleRefinePrompt(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID, stepID, ok := strings.Cut(r.URL.Query().Get("step"), "/")

	var promptText string
	var promptErr error
	found := false

	storeOf(r).Read(func(data *models.Data) {
		targetCase := data.FindCase(caseID)
		if targetCase == nil {
			return
		}
		for actionIndex := range targetCase.Actions {
			action := &targetCase.Actions[actionIndex]
			if action.ID != actionID {
				continue
			}
			for stepIndex := range action.Steps {
				if action.Steps[stepIndex].ID == stepID {
					found = true
					promptText, promptErr = services.BuildRefinePrompt(s.prompts.Refine, *targetCase, *action, stepIndex)
					return
				}
			}
		}
	})

	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	if promptErr != nil {
		http.Error(w, promptErr.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "case", "refine-prompt-box", struct{ Prompt, ActionID string }{Prompt: promptText, ActionID: actionID})
}

func (s *Server) handleRefineImport(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID, stepID, _ := strings.Cut(r.FormValue("step"), "/")
	preview := RefinePreview{CaseID: caseID, ActionID: actionID, StepID: stepID, RawReply: r.FormValue("reply")}
	if actionID == "" || stepID == "" {
		preview.Error = "сначала выбери шаг"
		s.render(w, "case", "refine-preview", preview)
		return
	}

	reply, err := services.ParseRefineReply(preview.RawReply)
	if err == nil {
		var targetCase models.Case
		caseFound := false
		storeOf(r).Read(func(data *models.Data) {
			if c := data.FindCase(caseID); c != nil {
				targetCase, caseFound = *c, true
			}
		})
		if !caseFound {
			http.NotFound(w, r)
			return
		}
		err = reply.Validate(targetCase, actionID, stepID)
	}
	if err != nil {
		preview.Error = err.Error()
		s.render(w, "case", "refine-preview", preview)
		return
	}
	preview.Steps = reply.Steps
	s.render(w, "case", "refine-preview", preview)
}

func (s *Server) handleRefineApply(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actionID, stepID, _ := strings.Cut(r.FormValue("step"), "/")

	selected := map[int]bool{}
	for _, rawIndex := range r.Form["keep"] {
		if index, err := strconv.Atoi(rawIndex); err == nil {
			selected[index] = true
		}
	}

	reply, err := services.ParseRefineReply(r.FormValue("reply"))
	if err == nil {
		err = storeOf(r).Update(func(data *models.Data) error {
			targetCase := data.FindCase(caseID)
			if targetCase == nil {
				return models.ErrCaseNotFound
			}
			if err := reply.Validate(*targetCase, actionID, stepID); err != nil {
				return err
			}
			var chosenTitles []string
			for index, title := range reply.Steps {
				if selected[index] {
					chosenTitles = append(chosenTitles, strings.TrimSpace(title))
				}
			}
			if len(chosenTitles) == 0 {
				return errors.New("выбери хотя бы один под-шаг")
			}
			for actionIndex := range targetCase.Actions {
				action := &targetCase.Actions[actionIndex]
				if action.ID != actionID {
					continue
				}
				for stepIndex := range action.Steps {
					if action.Steps[stepIndex].ID != stepID {
						continue
					}
					newSteps := make([]models.Step, 0, len(chosenTitles))
					for _, title := range chosenTitles {
						newSteps = append(newSteps, models.Step{ID: models.NewID("s"), Title: title})
					}
					action.Steps = slices.Replace(action.Steps, stepIndex, stepIndex+1, newSteps...)
					return nil
				}
				return models.ErrStepNotFound
			}
			return models.ErrActionNotFound
		})
	}

	notice := ""
	if err != nil {
		notice = err.Error()
	}
	s.renderCaseBody(w, r, caseID, notice)
}
