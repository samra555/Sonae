package main

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"
)

type Server struct {
	files          fs.FS
	accounts       *Accounts
	pageTemplates  map[string]*template.Template
	promptTemplate *texttemplate.Template
	risksTemplate  *texttemplate.Template
}

func NewServer(accounts *Accounts, files fs.FS) (*Server, error) {
	templateFunctions := template.FuncMap{
		"percent":  func(probability float64) string { return fmt.Sprintf("%.1f%%", probability*100) },
		"pctLabel": pctLabel,
		"deltaPoints": func(points *float64) string {
			if points == nil {
				return "—"
			}
			return fmt.Sprintf("%+.1f п.п.", *points)
		},
		"join":         strings.Join,
		"numStr":       numStr,
		"optNum":       optNum,
		"percentValue": percentValue,
		"stepKey":      stepKey,
	}

	pageTemplates := map[string]*template.Template{}
	for _, pageName := range []string{"index", "case", "login"} {
		parsedPage, err := template.New("").Funcs(templateFunctions).
			ParseFS(files, "templates/layout.html", "templates/"+pageName+".html")
		if err != nil {
			return nil, err
		}
		pageTemplates[pageName] = parsedPage
	}

	promptTemplate, err := texttemplate.ParseFS(files, "prompt.tmpl")
	if err != nil {
		return nil, err
	}
	risksTemplate, err := texttemplate.ParseFS(files, "risks.tmpl")
	if err != nil {
		return nil, err
	}
	return &Server{files: files, accounts: accounts, pageTemplates: pageTemplates, promptTemplate: promptTemplate, risksTemplate: risksTemplate}, nil
}

func (s *Server) Routes() http.Handler {
	public := http.NewServeMux()
	public.Handle("GET /static/", http.FileServerFS(s.files))
	public.HandleFunc("GET /login", s.handleLoginPage)
	public.HandleFunc("POST /login", s.handleLogin)
	public.HandleFunc("POST /register", s.handleRegister)
	public.HandleFunc("POST /logout", s.handleLogout)

	// Everything else works on one account's data and needs a session.
	mux := http.NewServeMux()
	public.Handle("/", s.requireLogin(mux))

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("POST /cases", s.handleCreateCase)
	mux.HandleFunc("GET /cases/{id}", s.handleCase)
	mux.HandleFunc("POST /cases/{id}/actions", s.handleCreateAction)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/toggle", s.handleToggleAction)
	mux.HandleFunc("GET /cases/{id}/prompt", s.handlePrompt)
	mux.HandleFunc("POST /cases/{id}/import", s.handleImport)
	mux.HandleFunc("POST /cases/{id}/apply", s.handleApply)
	mux.HandleFunc("POST /cases/{id}/update", s.handleUpdateCase)
	mux.HandleFunc("POST /cases/{id}/delete", s.handleDeleteCase)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/update", s.handleUpdateAction)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/delete", s.handleDeleteAction)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/links", s.handleLinks)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/accept", s.handleAcceptEstimate)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/steps", s.handleCreateStep)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/steps/{stepID}/toggle", s.handleToggleStep)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/steps/{stepID}/delete", s.handleDeleteStep)
	mux.HandleFunc("POST /profile", s.handleUpdateProfile)
	mux.HandleFunc("GET /risks/prompt", s.handleRisksPrompt)
	mux.HandleFunc("POST /risks/import", s.handleRisksImport)
	mux.HandleFunc("POST /risks/apply", s.handleRisksApply)
	return public
}

// ---- view models ----

type IndexRow struct {
	Kind                         Kind
	ID, Title, Category, Horizon string
	Base, Current                float64
	DoneCount, TotalCount        int
	ProgressPercent              int
	DeltaPoints                  float64
	Improved                     bool
}

type IndexView struct {
	Login              string
	Profile            Profile
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
	Action
	Blocked         bool
	BlockReason     string
	MyPercent       *float64
	LLMPercent      *float64
	CanAccept       bool
	DeltaPoints     *float64
	Good            bool
	OverlapTitles   []string
	RequiresChips   []LinkOption
	ExcludesChips   []LinkOption
	RequiresOptions []LinkGroup
	ExcludesOptions []LinkGroup

	benefit float64
}

type CaseView struct {
	Login   string
	Case    Case
	Current float64
	Actions []ActionView
	Notice  string
	Profile Profile
}

type PreviewRow struct {
	ActionID   string
	Title      string
	MyPercent  *float64
	LLMPercent *float64
	Estimate   ActionEstimate
	Applicable bool
}

type SuggestedRow struct {
	Index int
	SuggestedAction
}

type ImportPreview struct {
	CaseID    string
	RawReply  string
	Error     string
	Rows      []PreviewRow
	Suggested []SuggestedRow
	Comment   string
}

func linkChips(data *Data, linkedIDs []string) []LinkOption {
	chips := make([]LinkOption, 0, len(linkedIDs))
	for _, linkedID := range linkedIDs {
		chips = append(chips, LinkOption{ID: linkedID, Title: data.titleOf(linkedID)})
	}
	return chips
}

// linkGroups lists every action that can still be linked, the current case first, then the others by title.
func linkGroups(data *Data, currentCaseID, selfID string, alreadyLinked []string) []LinkGroup {
	var groups []LinkGroup
	appendGroup := func(groupTitle string, sourceCase Case) {
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

	if currentCase := data.findCase(currentCaseID); currentCase != nil {
		appendGroup("Этот кейс", *currentCase)
	}
	for _, otherCase := range data.Cases {
		if otherCase.ID != currentCaseID {
			appendGroup(otherCase.Title, otherCase)
		}
	}
	return groups
}

func buildCaseView(data *Data, caseID, notice string) (CaseView, bool) {
	foundCase := data.findCase(caseID)
	if foundCase == nil {
		return CaseView{}, false
	}

	actionViews := make([]ActionView, 0, len(foundCase.Actions))
	for _, action := range foundCase.Actions {
		actionView := ActionView{
			Action:          action,
			MyPercent:       optionalPercent(action.MyMultiplier),
			LLMPercent:      optionalPercent(action.LLMMultiplier),
			CanAccept:       action.LLMMultiplier != nil && !sameNumber(action.LLMMultiplier, action.MyMultiplier),
			RequiresChips:   linkChips(data, action.Requires),
			ExcludesChips:   linkChips(data, action.Excludes),
			RequiresOptions: linkGroups(data, foundCase.ID, action.ID, action.Requires),
			ExcludesOptions: linkGroups(data, foundCase.ID, action.ID, action.Excludes),
		}

		if !action.Done {
			actionView.BlockReason = data.BlockReason(&action)
			actionView.Blocked = actionView.BlockReason != ""
		}
		if action.MyMultiplier != nil {
			deltaPoints := foundCase.BaseProbability * (*action.MyMultiplier - 1) * 100
			actionView.DeltaPoints = &deltaPoints
			actionView.benefit = deltaPoints
			if foundCase.Kind == KindRisk {
				actionView.benefit = -deltaPoints
			}
			actionView.Good = actionView.benefit > 0
		}
		for _, overlapID := range action.OverlapsWith {
			actionView.OverlapTitles = append(actionView.OverlapTitles, data.titleOf(overlapID))
		}
		actionViews = append(actionViews, actionView)
	}

	// Biggest benefit first; actions without an estimate go last. The sort is stable,
	// so equal effects keep their order from the file.
	slices.SortStableFunc(actionViews, func(first, second ActionView) int {
		if (first.DeltaPoints == nil) != (second.DeltaPoints == nil) {
			if first.DeltaPoints == nil {
				return 1
			}
			return -1
		}
		return cmp.Compare(second.benefit, first.benefit)
	})

	return CaseView{
		Case:    *foundCase,
		Current: foundCase.CurrentProbability(),
		Actions: actionViews,
		Notice:  notice,
		Profile: data.Profile,
	}, true
}

func buildPreviewRows(targetCase Case, reply AssistantReply) []PreviewRow {
	actionsByID := map[string]Action{}
	for _, action := range targetCase.Actions {
		actionsByID[action.ID] = action
	}

	rows := make([]PreviewRow, 0, len(reply.Actions))
	for _, estimate := range reply.Actions {
		action := actionsByID[estimate.ID]
		rows = append(rows, PreviewRow{
			ActionID:   action.ID,
			Title:      action.Title,
			MyPercent:  optionalPercent(action.MyMultiplier),
			LLMPercent: optionalPercent(action.LLMMultiplier),
			Estimate:   estimate,
			Applicable: estimate.EffectPercent != nil,
		})
	}
	return rows
}

// ---- helpers ----

func (s *Server) render(w http.ResponseWriter, pageName, templateName string, viewData any) {
	var renderedPage bytes.Buffer
	if err := s.pageTemplates[pageName].ExecuteTemplate(&renderedPage, templateName, viewData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = renderedPage.WriteTo(w)
}

func (s *Server) renderCaseBody(w http.ResponseWriter, r *http.Request, caseID, notice string) bool {
	var caseView CaseView
	var caseFound bool
	storeOf(r).Read(func(data *Data) {
		caseView, caseFound = buildCaseView(data, caseID, notice)
	})
	if !caseFound {
		http.Error(w, errCaseNotFound.Error(), http.StatusNotFound)
		return false
	}
	s.render(w, "case", "case-body", caseView)
	return true
}

func newID(prefix string) string {
	randomBytes := make([]byte, 4)
	_, _ = rand.Read(randomBytes)
	return prefix + "-" + hex.EncodeToString(randomBytes)
}

// parseNumber accepts what people actually type: a comma, a typographic minus, a percent sign.
func parseNumber(rawNumber string) (float64, error) {
	normalized := strings.NewReplacer(",", ".", "−", "-", "%", "", " ", "").Replace(rawNumber)
	return strconv.ParseFloat(normalized, 64)
}

func parseOptionalNumber(rawNumber string) (*float64, error) {
	if strings.TrimSpace(rawNumber) == "" {
		return nil, nil
	}
	number, err := parseNumber(rawNumber)
	if err != nil {
		return nil, err
	}
	return &number, nil
}

func actionFromForm(r *http.Request) (Action, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return Action{}, errors.New("у действия должно быть название")
	}

	effectPercent, err := parseOptionalNumber(r.FormValue("effect_percent"))
	if err != nil || !validEffectPercent(effectPercent) {
		return Action{}, fmt.Errorf("прикидка: число от %.0f до %.0f, например −10 или +30", minEffectPercent, maxEffectPercent)
	}
	return Action{ID: newID("a"), Title: title, MyMultiplier: multiplierFromPercent(effectPercent)}, nil
}

// ---- handlers ----

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	indexView := IndexView{Login: loginOf(r)}
	storeOf(r).Read(func(data *Data) {
		indexView.Profile = data.Profile
		for _, listedCase := range data.Cases {
			doneCount := 0
			for _, action := range listedCase.Actions {
				if action.Done {
					doneCount++
				}
			}
			row := IndexRow{
				ID: listedCase.ID, Kind: listedCase.Kind, Title: listedCase.Title, Category: listedCase.Category, Horizon: listedCase.Horizon,
				Base: listedCase.BaseProbability, Current: listedCase.CurrentProbability(),
				DoneCount: doneCount, TotalCount: len(listedCase.Actions),
				ProgressPercent: listedCase.ProgressPercent(),
				DeltaPoints:     listedCase.DeltaPoints(),
				Improved:        listedCase.Improved(),
			}
			if listedCase.Kind == KindRisk {
				indexView.Risks = append(indexView.Risks, row)
				continue
			}
			indexView.Aspirations = append(indexView.Aspirations, row)
		}
	})
	s.render(w, "index", "layout", indexView)
}

func (s *Server) handleCase(w http.ResponseWriter, r *http.Request) {
	var caseView CaseView
	var caseFound bool
	storeOf(r).Read(func(data *Data) {
		caseView, caseFound = buildCaseView(data, r.PathValue("id"), "")
	})
	if !caseFound {
		http.NotFound(w, r)
		return
	}
	caseView.Login = loginOf(r)
	s.render(w, "case", "layout", caseView)
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	baseProbabilityPercent, err := parseNumber(r.FormValue("base_probability"))
	if err != nil {
		http.Error(w, "базовая вероятность: нужно число в процентах", http.StatusBadRequest)
		return
	}

	newCase := Case{
		ID:              newID("c"),
		Kind:            Kind(r.FormValue("kind")),
		Category:        strings.TrimSpace(r.FormValue("category")),
		Title:           strings.TrimSpace(r.FormValue("title")),
		Description:     strings.TrimSpace(r.FormValue("description")),
		Horizon:         strings.TrimSpace(r.FormValue("horizon")),
		BaseProbability: baseProbabilityPercent / 100,
		BaseSource:      strings.TrimSpace(r.FormValue("base_source")),
		Actions:         []Action{},
	}
	err = storeOf(r).Update(func(data *Data) error {
		data.Cases = append(data.Cases, newCase)
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/cases/"+newCase.ID, http.StatusSeeOther)
}

func (s *Server) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")

	newAction, err := actionFromForm(r)
	if err == nil {
		err = storeOf(r).Update(func(data *Data) error {
			targetCase := data.findCase(caseID)
			if targetCase == nil {
				return errCaseNotFound
			}
			targetCase.Actions = append(targetCase.Actions, newAction)
			return nil
		})
	}

	notice := ""
	if err != nil {
		notice = err.Error()
	}
	s.renderCaseBody(w, r, caseID, notice)
}

func (s *Server) handleToggleAction(w http.ResponseWriter, r *http.Request) {
	caseID := r.PathValue("id")
	actionID := r.PathValue("actionID")

	err := storeOf(r).Update(func(data *Data) error {
		action := data.findAction(actionID)
		if action == nil {
			return errActionNotFound
		}
		return data.ToggleAction(action)
	})

	notice := ""
	if err != nil {
		notice = err.Error()
	}
	s.renderCaseBody(w, r, caseID, notice)
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	var promptText string
	var promptErr error
	caseFound := false
	storeOf(r).Read(func(data *Data) {
		targetCase := data.findCase(r.PathValue("id"))
		if targetCase == nil {
			return
		}
		caseFound = true
		promptText, promptErr = buildPrompt(s.promptTemplate, data.Profile, *targetCase)
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

	var targetCase Case
	caseFound := false
	storeOf(r).Read(func(data *Data) {
		if foundCase := data.findCase(caseID); foundCase != nil {
			targetCase, caseFound = *foundCase, true
		}
	})
	if !caseFound {
		http.NotFound(w, r)
		return
	}

	preview := ImportPreview{CaseID: caseID, RawReply: r.FormValue("reply")}

	reply, err := parseAssistantReply(preview.RawReply)
	if err == nil {
		err = reply.Validate(targetCase)
	}
	if err != nil {
		preview.Error = err.Error()
		s.render(w, "case", "import-preview", preview)
		return
	}

	preview.Rows = buildPreviewRows(targetCase, reply)
	preview.Comment = reply.BaseProbabilityComment
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

	selection := ApplySelection{ActionIDs: map[string]bool{}, SuggestedIndexes: map[int]bool{}, StepKeys: map[string]bool{}}
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

	reply, err := parseAssistantReply(r.FormValue("reply"))
	if err == nil {
		err = storeOf(r).Update(func(data *Data) error {
			targetCase := data.findCase(caseID)
			if targetCase == nil {
				return errCaseNotFound
			}
			if err := reply.Validate(*targetCase); err != nil {
				return err
			}
			applyReply(targetCase, reply, selection, time.Now().Format("2006-01-02"))
			return nil
		})
	}

	notice := ""
	if err != nil {
		notice = err.Error()
	}
	if s.renderCaseBody(w, r, caseID, notice) {
		// Out-of-band swap: clears the import preview next to the refreshed case body.
		_, _ = io.WriteString(w, `<div id="import-result" hx-swap-oob="true"></div>`)
	}
}
