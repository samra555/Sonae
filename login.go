package main

import (
	"context"
	"net/http"
	"time"
)

const sessionCookieName = "session"

type contextKey int

const (
	loginContextKey contextKey = iota
	storeContextKey
)

// storeOf is the data of the account that made the request; requireLogin puts it there.
func storeOf(r *http.Request) *Store {
	return r.Context().Value(storeContextKey).(*Store)
}

func loginOf(r *http.Request) string {
	return r.Context().Value(loginContextKey).(string)
}

// requireLogin lets through only requests with a valid session and attaches that account's data.
func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login := ""
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			login = s.accounts.LoginFromSession(cookie.Value)
		}
		store, err := s.accounts.StoreFor(login)
		if login == "" || err != nil {
			redirectToLogin(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), loginContextKey, login)
		ctx = context.WithValue(ctx, storeContextKey, store)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	// An htmx request would swap the login page into a fragment; ask htmx for a full redirect instead.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// SameSite=Strict keeps other sites from sending requests with this cookie (no CSRF),
// HttpOnly keeps it away from page scripts.
func setSessionCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

type LoginView struct {
	HasUsers bool
	Login    string
	Error    string
}

func (s *Server) renderLogin(w http.ResponseWriter, view LoginView) {
	view.HasUsers = s.accounts.HasUsers()
	s.render(w, "login", "login-page", view)
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.renderLogin(w, LoginView{})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	login := r.FormValue("login")
	if !s.accounts.Authenticate(login, r.FormValue("password")) {
		s.renderLogin(w, LoginView{Login: login, Error: "неверный логин или пароль"})
		return
	}
	s.startSession(w, r, login)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	login := r.FormValue("login")
	password := r.FormValue("password")
	if password != r.FormValue("password_repeat") {
		s.renderLogin(w, LoginView{Login: login, Error: "пароли не совпадают"})
		return
	}
	if err := s.accounts.Register(login, password); err != nil {
		s.renderLogin(w, LoginView{Login: login, Error: err.Error()})
		return
	}
	s.startSession(w, r, login)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, login string) {
	sessionValue, expiresAt := s.accounts.NewSessionValue(login)
	setSessionCookie(w, sessionValue, expiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	setSessionCookie(w, "", time.Unix(0, 0))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
