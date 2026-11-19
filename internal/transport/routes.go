package transport

import "net/http"

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
	mux.HandleFunc("GET /cases/{id}/refine/prompt", s.handleRefinePrompt)
	mux.HandleFunc("POST /cases/{id}/refine/import", s.handleRefineImport)
	mux.HandleFunc("POST /cases/{id}/refine/apply", s.handleRefineApply)
	mux.HandleFunc("POST /cases/{id}/update", s.handleUpdateCase)
	mux.HandleFunc("POST /cases/{id}/delete", s.handleDeleteCase)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/update", s.handleUpdateAction)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/notes", s.handleSaveNotes)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/delete", s.handleDeleteAction)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/links", s.handleLinks)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/accept", s.handleAcceptEstimate)
	mux.HandleFunc("POST /cases/{id}/accept-all", s.handleAcceptAllEstimates)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/steps", s.handleCreateStep)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/steps/{stepID}/toggle", s.handleToggleStep)
	mux.HandleFunc("POST /cases/{id}/actions/{actionID}/steps/{stepID}/delete", s.handleDeleteStep)
	mux.HandleFunc("POST /profile", s.handleUpdateProfile)
	mux.HandleFunc("GET /risks/prompt", s.handleRisksPrompt)
	mux.HandleFunc("POST /risks/import", s.handleRisksImport)
	mux.HandleFunc("POST /risks/apply", s.handleRisksApply)
	return public
}
