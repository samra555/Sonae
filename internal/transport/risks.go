package transport

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

type RiskRow struct {
	Index int
	services.SuggestedRisk
	// Duplicate is set when a case with the same title already exists; such rows start unchecked.
	Duplicate bool
}

type RisksPreview struct {
	RawReply string
	Error    string
	Rows     []RiskRow
}

func (s *Server) handleRisksPrompt(w http.ResponseWriter, r *http.Request) {
	var promptText string
	var promptErr error
	storeOf(r).Read(func(data *models.Data) {
		promptText, promptErr = services.BuildRisksPrompt(s.prompts.Risks, data, r.FormValue("focus"))
	})
	if promptErr != nil {
		http.Error(w, promptErr.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "index", "prompt-box", struct{ Prompt string }{Prompt: promptText})
}

func (s *Server) handleRisksImport(w http.ResponseWriter, r *http.Request) {
	preview := RisksPreview{RawReply: r.FormValue("reply")}

	reply, err := services.ParseRisksReply(preview.RawReply)
	if err != nil {
		preview.Error = err.Error()
		s.render(w, "index", "risks-preview", preview)
		return
	}

	storeOf(r).Read(func(data *models.Data) {
		for riskIndex, risk := range reply.Risks {
			preview.Rows = append(preview.Rows, RiskRow{
				Index: riskIndex, SuggestedRisk: risk, Duplicate: services.HasCaseTitled(data, risk.Title),
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

	reply, err := services.ParseRisksReply(r.FormValue("reply"))
	if err == nil && len(r.Form["add"]) == 0 {
		err = errors.New("не выбран ни один риск")
	}
	if err == nil {
		estimatedAt := time.Now().Format("2006-01-02")
		err = storeOf(r).Update(func(data *models.Data) error {
			for _, rawIndex := range r.Form["add"] {
				riskIndex, err := strconv.Atoi(rawIndex)
				if err != nil || riskIndex < 0 || riskIndex >= len(reply.Risks) {
					return errors.New("неверный номер риска")
				}
				data.Cases = append(data.Cases, services.CaseFromRisk(reply.Risks[riskIndex], estimatedAt))
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
