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

func applyActionForm(action *Action, r *http.Request) error {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return errors.New("у действия должно быть название")
	}

	effectPercent, err := parseOptionalNumber(r.FormValue("effect_percent"))
	if err != nil || !validEffectPercent(effectPercent) {
		return fmt.Errorf("прикидка: число от %.0f до %.0f, например −10 или +30", minEffectPercent, maxEffectPercent)
	}

	action.Title = title
	action.MyMultiplier = multiplierFromPercent(effectPercent)
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

	baseProbabilityPercent, err := parseNumber(r.FormValue("base_probability"))
	if err != nil {
		s.renderCaseBody(w, caseID, "базовая вероятность: нужно число в процентах")
		return
	}

	err = s.store.Update(func(data *Data) error {
		targetCase := data.findCase(caseID)
		if targetCase == nil {
			return errCaseNotFound
		}
		targetCase.Kind = Kind(r.FormValue("kind"))
		targetCase.Category = strings.TrimSpace(r.FormValue("category"))
		targetCase.Title = strings.TrimSpace(r.FormValue("title"))
		targetCase.Horizon = strings.TrimSpace(r.FormValue("horizon"))
		targetCase.BaseProbability = baseProbabilityPercent / 100
		targetCase.BaseSource = strings.TrimSpace(r.FormValue("base_source"))
		return nil
	})
	s.renderCaseBody(w, caseID, noticeOf(err))
}

func (s *Server) handleDeleteCase(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")

	err := s.store.Update(func(data *Data) error {
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
		s.renderCaseBody(w, caseID, err.Error())
		return
	}

	// htmx performs a client-side redirect when it sees this header.
	w.Header().Set("HX-Redirect", "/")
}

func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := s.store.Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		return applyActionForm(action, r)
	})
	s.renderCaseBody(w, caseID, noticeOf(err))
}

func (s *Server) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := s.store.Update(func(data *Data) error {
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
	s.renderCaseBody(w, caseID, noticeOf(err))
}

// handleLinks adds or removes one requires/excludes link; the target comes from a dropdown of titles.
func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")
	linkKind := r.FormValue("kind")
	operation := r.FormValue("op")
	targetID := r.FormValue("target")

	err := s.store.Update(func(data *Data) error {
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
	s.renderCaseBody(w, caseID, noticeOf(err))
}

// handleAcceptEstimate copies the stored neural-net estimate into the user's own estimate.
func (s *Server) handleAcceptEstimate(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := s.store.Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		if action.LLMMultiplier == nil {
			return errors.New("у этого действия нет оценки нейросети")
		}
		action.MyMultiplier = action.LLMMultiplier
		return nil
	})
	s.renderCaseBody(w, caseID, noticeOf(err))
}

func writeProfileStatus(w http.ResponseWriter, message string, isError bool) {
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
		writeProfileStatus(w, "возраст: целое число", true)
		return
	}

	err = s.store.Update(func(data *Data) error {
		data.Profile = Profile{Age: age, Country: strings.TrimSpace(r.FormValue("country"))}
		return nil
	})
	if err != nil {
		writeProfileStatus(w, err.Error(), true)
		return
	}
	writeProfileStatus(w, "Сохранено ✓", false)
}
