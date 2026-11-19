package transport

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"gotovnost/internal/database"
	"gotovnost/internal/models"
	"gotovnost/internal/services"
)

type Server struct {
	files         fs.FS
	accounts      *database.Accounts
	pageTemplates map[string]*template.Template
	prompts       *services.ParsedPrompts
}

func NewServer(accounts *database.Accounts, files fs.FS) (*Server, error) {
	templateFunctions := template.FuncMap{
		"percent":     func(probability float64) string { return fmt.Sprintf("%.1f%%", probability*100) },
		"pctLabel":    models.PctLabel,
		"weightLabel": models.WeightLabel,
		// weightInput bundles what the "weight-input" template needs: templates take one argument.
		"weightInput": func(targetCase models.Case, weight *float64) any {
			return struct {
				Case   models.Case
				Weight *float64
			}{targetCase, weight}
		},
		"join":         strings.Join,
		"numStr":       models.NumStr,
		"optNum":       models.OptNum,
		"percentValue": models.PercentValue,
		"stepKey":      services.StepKey,
		"inc":          func(n int) int { return n + 1 },
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

	prompts, err := services.ParsePrompts()
	if err != nil {
		return nil, err
	}
	return &Server{files: files, accounts: accounts, pageTemplates: pageTemplates, prompts: prompts}, nil
}

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
	storeOf(r).Read(func(data *models.Data) {
		caseView, caseFound = BuildCaseView(data, caseID, notice)
	})
	if !caseFound {
		http.Error(w, models.ErrCaseNotFound.Error(), http.StatusNotFound)
		return false
	}
	s.render(w, "case", "case-body", caseView)
	return true
}
