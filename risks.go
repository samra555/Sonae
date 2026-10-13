package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// suggestedRiskCount is how many main risks the neural net is asked for.
const suggestedRiskCount = 6

// ---- request (what we send to the neural net) ----

type existingCaseSummary struct {
	Title    string `json:"title"`
	Kind     Kind   `json:"kind"`
	Category string `json:"category"`
}

type risksRequest struct {
	Task          string                `json:"task"`
	Count         int                   `json:"count"`
	Focus         string                `json:"focus"`
	Profile       Profile               `json:"profile"`
	ExistingCases []existingCaseSummary `json:"existing_cases"`
}

// buildRisksPrompt sends the profile and the existing cases, so the neural net does not repeat them.
// focus is the user's optional clarification of what to concentrate on; it is not stored.
func (s *Server) buildRisksPrompt(data *Data, focus string) (string, error) {
	request := risksRequest{
		Task:          "suggest_main_risks",
		Count:         suggestedRiskCount,
		Focus:         strings.TrimSpace(focus),
		Profile:       data.Profile,
		ExistingCases: []existingCaseSummary{},
	}
	for _, existingCase := range data.Cases {
		request.ExistingCases = append(request.ExistingCases, existingCaseSummary{
			Title: existingCase.Title, Kind: existingCase.Kind, Category: existingCase.Category,
		})
	}
	return renderPrompt(s.risksTemplate, request, false)
}

// ---- reply (what the neural net sends back) ----

type RiskAction struct {
	Title         string   `json:"title"`
	EffectPercent *float64 `json:"effect_percent"`
	Confidence    string   `json:"confidence"`
	Reasoning     string   `json:"reasoning"`
	Sources       []string `json:"sources"`
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

func parseRisksReply(rawReply string) (RisksReply, error) {
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
			if !validEffectPercent(action.EffectPercent) {
				return fmt.Errorf("риск №%d, действие №%d: effect_percent вне диапазона %.0f…%.0f",
					position, actionIndex+1, minEffectPercent, maxEffectPercent)
			}
			if !validConfidence(action.Confidence) {
				return fmt.Errorf("риск №%d, действие №%d: confidence должен быть low, medium или high", position, actionIndex+1)
			}
		}
	}
	return nil
}

// caseFromRisk turns a suggested risk into a new case. The neural net's effects go into
// llm_multiplier only, so nothing counts until the user accepts it, as with any other estimate.
func caseFromRisk(risk SuggestedRisk, estimatedAt string) Case {
	newCase := Case{
		ID:              newID("c"),
		Kind:            KindRisk,
		Category:        strings.TrimSpace(risk.Category),
		Title:           strings.TrimSpace(risk.Title),
		Description:     strings.TrimSpace(risk.Description),
		Horizon:         strings.TrimSpace(risk.Horizon),
		BaseProbability: *risk.BaseProbabilityPercent / 100,
		BaseSource:      strings.TrimSpace(risk.BaseSource),
		Actions:         []Action{},
	}
	for _, riskAction := range risk.Actions {
		newCase.Actions = append(newCase.Actions, Action{
			ID:            newID("a"),
			Title:         strings.TrimSpace(riskAction.Title),
			LLMMultiplier: multiplierFromPercent(riskAction.EffectPercent),
			Confidence:    riskAction.Confidence,
			Reasoning:     riskAction.Reasoning,
			Sources:       riskAction.Sources,
			EstimatedAt:   estimatedAt,
		})
	}
	return newCase
}

// ---- view models ----

type RiskRow struct {
	Index int
	SuggestedRisk
	// Duplicate is set when a case with the same title already exists; such rows start unchecked.
	Duplicate bool
}

type RisksPreview struct {
	RawReply string
	Error    string
	Rows     []RiskRow
}

func hasCaseTitled(data *Data, title string) bool {
	for _, existingCase := range data.Cases {
		if strings.EqualFold(strings.TrimSpace(existingCase.Title), strings.TrimSpace(title)) {
			return true
		}
	}
	return false
}

// ---- handlers ----

func (s *Server) handleRisksPrompt(w http.ResponseWriter, r *http.Request) {
	var promptText string
	var promptErr error
	storeOf(r).Read(func(data *Data) {
		promptText, promptErr = s.buildRisksPrompt(data, r.FormValue("focus"))
	})
	if promptErr != nil {
		http.Error(w, promptErr.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "index", "prompt-box", struct{ Prompt string }{Prompt: promptText})
}

func (s *Server) handleRisksImport(w http.ResponseWriter, r *http.Request) {
	preview := RisksPreview{RawReply: r.FormValue("reply")}

	reply, err := parseRisksReply(preview.RawReply)
	if err != nil {
		preview.Error = err.Error()
		s.render(w, "index", "risks-preview", preview)
		return
	}

	storeOf(r).Read(func(data *Data) {
		for riskIndex, risk := range reply.Risks {
			preview.Rows = append(preview.Rows, RiskRow{
				Index: riskIndex, SuggestedRisk: risk, Duplicate: hasCaseTitled(data, risk.Title),
			})
		}
	})
	s.render(w, "index", "risks-preview", preview)
}

// handleRisksApply only appends new cases: existing ones are never touched.
func (s *Server) handleRisksApply(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reply, err := parseRisksReply(r.FormValue("reply"))
	if err == nil && len(r.Form["add"]) == 0 {
		err = errors.New("не выбран ни один риск")
	}
	if err == nil {
		estimatedAt := time.Now().Format("2006-01-02")
		err = storeOf(r).Update(func(data *Data) error {
			for _, rawIndex := range r.Form["add"] {
				riskIndex, err := strconv.Atoi(rawIndex)
				if err != nil || riskIndex < 0 || riskIndex >= len(reply.Risks) {
					return errors.New("неверный номер риска")
				}
				data.Cases = append(data.Cases, caseFromRisk(reply.Risks[riskIndex], estimatedAt))
			}
			return nil
		})
	}
	if err != nil {
		writeStatus(w, err.Error(), true)
		return
	}

	// htmx performs a client-side redirect when it sees this header.
	w.Header().Set("HX-Redirect", "/")
}
