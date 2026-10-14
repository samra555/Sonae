package main

import (
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

func noticeOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// numStr uses the shortest exact representation, so a value shown in a form
// parses back to the identical number.
func numStr(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

func optNum(number *float64) string {
	if number == nil {
		return ""
	}
	return numStr(*number)
}

func percentValue(probability float64) string {
	return numStr(math.Round(probability*100*10000) / 10000)
}

func sameNumber(first, second *float64) bool {
	if first == nil || second == nil {
		return first == second
	}
	return *first == *second
}

// referencedBy finds an action (other than the ignored ones) that requires or excludes targetID.
func (d *Data) referencedBy(targetID string, ignoredIDs []string) *Action {
	for _, other := range d.allActions() {
		if slices.Contains(ignoredIDs, other.ID) {
			continue
		}
		if slices.Contains(other.Requires, targetID) || slices.Contains(other.Excludes, targetID) {
			return other
		}
	}
	return nil
}

// applyActionForm copies the title and the weight from the form. The allowed weights depend on
// the kind of the case, so the case is needed too.
func applyActionForm(action *Action, kind Kind, r *http.Request) error {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return errors.New("у действия должно быть название")
	}

	weight, err := parseOptionalNumber(r.FormValue("weight"))
	if err != nil || !validWeight(kind, weight) {
		return weightError(kind)
	}

	action.Title = title
	action.Weight = weight
	return nil
}

// caseOfAction finds the case that holds the action.
func (d *Data) caseOfAction(actionID string) *Case {
	for caseIndex := range d.Cases {
		for _, action := range d.Cases[caseIndex].Actions {
			if action.ID == actionID {
				return &d.Cases[caseIndex]
			}
		}
	}
	return nil
}

// applyCaseForm copies the case fields from the form. Only a risk has a base probability:
// an aspiration starts from 0% done, so its base fields are not read.
func applyCaseForm(targetCase *Case, r *http.Request) error {
	targetCase.Kind = Kind(r.FormValue("kind"))
	targetCase.Category = strings.TrimSpace(r.FormValue("category"))
	targetCase.Title = strings.TrimSpace(r.FormValue("title"))
	targetCase.Description = strings.TrimSpace(r.FormValue("description"))
	targetCase.Horizon = strings.TrimSpace(r.FormValue("horizon"))

	if targetCase.Kind != KindRisk {
		return nil
	}
	baseProbabilityPercent, err := parseNumber(r.FormValue("base_probability"))
	if err != nil {
		return errors.New("базовая вероятность: нужно число в процентах")
	}
	targetCase.BaseProbability = baseProbabilityPercent / 100
	targetCase.BaseSource = strings.TrimSpace(r.FormValue("base_source"))
	return nil
}

func linkedIDsOf(action *Action, linkKind string) (*[]string, error) {
	if linkKind == "requires" {
		return &action.Requires, nil
	}
	if linkKind == "excludes" {
		return &action.Excludes, nil
	}
	return nil, errors.New("неизвестный тип связи")
}

func (s *Server) handleUpdateCase(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")

	err := storeOf(r).Update(func(data *Data) error {
		targetCase := data.findCase(caseID)
		if targetCase == nil {
			return errCaseNotFound
		}
		return applyCaseForm(targetCase, r)
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

func (s *Server) handleDeleteCase(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")

	err := storeOf(r).Update(func(data *Data) error {
		for caseIndex := range data.Cases {
			if data.Cases[caseIndex].ID != caseID {
				continue
			}

			ownActionIDs := []string{}
			for _, action := range data.Cases[caseIndex].Actions {
				ownActionIDs = append(ownActionIDs, action.ID)
			}
			for _, ownActionID := range ownActionIDs {
				if referencing := data.referencedBy(ownActionID, ownActionIDs); referencing != nil {
					return fmt.Errorf("нельзя удалить: «%s» из другого кейса ссылается на «%s»",
						referencing.Title, data.titleOf(ownActionID))
				}
			}

			data.Cases = slices.Delete(data.Cases, caseIndex, caseIndex+1)
			return nil
		}
		return errCaseNotFound
	})
	if err != nil {
		s.renderCaseBody(w, r, caseID, err.Error())
		return
	}

	// htmx performs a client-side redirect when it sees this header.
	w.Header().Set("HX-Redirect", "/")
}

func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		return applyActionForm(action, data.caseOfAction(actionID).Kind, r)
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

func (s *Server) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := storeOf(r).Update(func(data *Data) error {
		for caseIndex := range data.Cases {
			actions := data.Cases[caseIndex].Actions
			for actionIndex := range actions {
				if actions[actionIndex].ID != actionID {
					continue
				}
				if referencing := data.referencedBy(actionID, []string{actionID}); referencing != nil {
					return fmt.Errorf("нельзя удалить: на это действие ссылается «%s»", referencing.Title)
				}
				data.Cases[caseIndex].Actions = slices.Delete(actions, actionIndex, actionIndex+1)
				return nil
			}
		}
		return errActionNotFound
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

// handleLinks adds or removes one requires/excludes link; the target comes from a dropdown of titles.
func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")
	linkKind := r.FormValue("kind")
	operation := r.FormValue("op")
	targetID := r.FormValue("target")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		linkedIDs, err := linkedIDsOf(action, linkKind)
		if err != nil {
			return err
		}
		if targetID == "" {
			return errors.New("сначала выбери действие в списке")
		}
		if targetID == actionID {
			return errors.New("нельзя связать действие с самим собой")
		}

		switch operation {
		case "add":
			if !slices.Contains(*linkedIDs, targetID) {
				*linkedIDs = append(*linkedIDs, targetID)
			}
		case "remove":
			*linkedIDs = slices.DeleteFunc(*linkedIDs, func(linkedID string) bool { return linkedID == targetID })
		default:
			return errors.New("неизвестная операция со связью")
		}

		if !action.Done {
			return nil
		}
		if reason := data.BlockReason(action); reason != "" {
			return errors.New("действие выполнено, а связи не выполняются (" + reason + "): сначала сними отметку")
		}
		return nil
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

// handleAcceptEstimate copies the stored neural-net weight into the user's own weight.
func (s *Server) handleAcceptEstimate(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		if action.LLMWeight == nil {
			return errors.New("у этого действия нет оценки нейросети")
		}
		action.Weight = action.LLMWeight
		return nil
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

// handleAcceptAllEstimates takes the neural net's weight for every action of the case that has one.
// It is handy for an aspiration, whose weights only make sense together (they add up to 100).
func (s *Server) handleAcceptAllEstimates(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")

	err := storeOf(r).Update(func(data *Data) error {
		targetCase := data.findCase(caseID)
		if targetCase == nil {
			return errCaseNotFound
		}
		for actionIndex := range targetCase.Actions {
			action := &targetCase.Actions[actionIndex]
			if action.LLMWeight != nil {
				action.Weight = action.LLMWeight
			}
		}
		return nil
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

func (s *Server) handleCreateStep(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		var stepTitles []string
		for _, line := range strings.Split(r.FormValue("titles"), "\n") {
			if title := strings.TrimSpace(line); title != "" {
				stepTitles = append(stepTitles, title)
			}
		}
		if len(stepTitles) == 0 {
			return errors.New("у шага должно быть название")
		}
		addSteps(action, stepTitles)
		return nil
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

func (s *Server) handleToggleStep(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		step := action.findStep(r.PathValue("stepID"))
		if step == nil {
			return errStepNotFound
		}
		return data.ToggleStep(action, step)
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

func (s *Server) handleDeleteStep(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")
	stepID := r.PathValue("stepID")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		if action.findStep(stepID) == nil {
			return errStepNotFound
		}
		action.Steps = slices.DeleteFunc(action.Steps, func(step Step) bool { return step.ID == stepID })
		return nil
	})
	s.renderCaseBody(w, r, caseID, noticeOf(err))
}

// writeStatus answers an htmx form with a short coloured message.
func writeStatus(w http.ResponseWriter, message string, isError bool) {
	cssClass := "ok"
	if isError {
		cssClass = "err"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span class="%s">%s</span>`, cssClass, html.EscapeString(message))
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	age, err := strconv.Atoi(strings.TrimSpace(r.FormValue("age")))
	if err != nil {
		writeStatus(w, "возраст: целое число", true)
		return
	}

	err = storeOf(r).Update(func(data *Data) error {
		data.Profile = Profile{
			Age:           age,
			Sex:           r.FormValue("sex"),
			Country:       strings.TrimSpace(r.FormValue("country")),
			City:          strings.TrimSpace(r.FormValue("city")),
			Profession:    strings.TrimSpace(r.FormValue("profession")),
			MaritalStatus: r.FormValue("marital_status"),
			Children:      r.FormValue("children"),
			Health:        strings.TrimSpace(r.FormValue("health")),
			About:         strings.TrimSpace(r.FormValue("about")),
		}
		return nil
	})
	if err != nil {
		writeStatus(w, err.Error(), true)
		return
	}
	writeStatus(w, "Сохранено ✓", false)
}
