package transport_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gotovnost/internal/database"
	"gotovnost/internal/transport"
)

func newTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	if err := accounts.Register("kirill", "password123"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv, err := transport.NewServer(accounts, transport.Files)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	return ts, client
}

func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	ts, client := newTestServer(t)
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("expected redirect to /login, got %s", resp.Request.URL.Path)
	}
}

func TestLogin(t *testing.T) {
	ts, client := newTestServer(t)
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"login": {"kirill"}, "password": {"password123"}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "неверный логин или пароль") {
		t.Fatal("login failed")
	}
}

func TestRefineFlow(t *testing.T) {
	ts, client := newTestServer(t)

	if _, err := client.PostForm(ts.URL+"/login", url.Values{"login": {"kirill"}, "password": {"password123"}}); err != nil {
		t.Fatalf("login: %v", err)
	}

	resp, err := client.PostForm(ts.URL+"/cases", url.Values{
		"kind": {"aspiration"}, "title": {"Test case"}, "category": {"test"}, "horizon": {"1 year"},
	})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	resp.Body.Close()
	caseID := regexp.MustCompile(`c-[0-9a-f]+`).FindString(resp.Request.URL.Path)
	if caseID == "" {
		t.Fatalf("could not find case id in %s", resp.Request.URL.Path)
	}

	actionResp := htmxPost(t, client, ts.URL+"/cases/"+caseID+"/actions", url.Values{"title": {"Action"}})
	actionID := regexp.MustCompile(`a-[0-9a-f]+`).FindString(actionResp)
	if actionID == "" {
		t.Fatal("could not find action id")
	}

	stepResp := htmxPost(t, client, ts.URL+"/cases/"+caseID+"/actions/"+actionID+"/steps", url.Values{"titles": {"step one"}})
	stepID := regexp.MustCompile(`s-[0-9a-f]+`).FindString(stepResp)
	if stepID == "" {
		t.Fatal("could not find step id")
	}

	promptResp, err := client.Get(ts.URL + "/cases/" + caseID + "/refine/prompt?step=" + actionID + "/" + stepID)
	if err != nil {
		t.Fatalf("refine prompt: %v", err)
	}
	promptBody, _ := io.ReadAll(promptResp.Body)
	promptResp.Body.Close()
	if !strings.Contains(string(promptBody), "target_step") {
		t.Fatal("refine prompt missing target_step")
	}

	reply := `{"action_id":"` + actionID + `","step_id":"` + stepID + `","steps":["sub a","sub b"]}`
	importResp := htmxPost(t, client, ts.URL+"/cases/"+caseID+"/refine/import", url.Values{
		"step": {actionID + "/" + stepID}, "reply": {reply},
	})
	if !strings.Contains(importResp, "sub a") {
		t.Fatalf("refine import should show sub-steps, got: %s", importResp)
	}

	estimate := `{"case_id":"` + caseID + `","actions":[],"suggested_actions":[],"comment":""}`
	crossResp := htmxPost(t, client, ts.URL+"/cases/"+caseID+"/refine/import", url.Values{
		"step": {actionID + "/" + stepID}, "reply": {estimate},
	})
	if !strings.Contains(crossResp, "notice") {
		t.Fatalf("expected schema error, got: %s", crossResp)
	}

	applyResp := htmxPost(t, client, ts.URL+"/cases/"+caseID+"/refine/apply", url.Values{
		"step": {actionID + "/" + stepID}, "reply": {reply}, "keep": {"0"},
	})
	if !strings.Contains(applyResp, "sub a") || strings.Contains(applyResp, "sub b") {
		t.Fatalf("apply should keep sub a and drop sub b, got: %s", applyResp)
	}
	if strings.Contains(applyResp, "step one") {
		t.Fatalf("apply should remove the original step, got: %s", applyResp)
	}
}

func htmxPost(t *testing.T, client *http.Client, endpoint string, form url.Values) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(body)
}
