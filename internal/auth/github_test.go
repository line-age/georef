package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/line-age/georef/internal/auth"
)

func TestRequireWrite_DisabledAuth_PassesThrough(t *testing.T) {
	authenticator := auth.New(auth.Config{}) // disabled: no ClientID

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/sheets", nil)
	rec := httptest.NewRecorder()
	authenticator.RequireWrite(next).ServeHTTP(rec, req)

	if !called {
		t.Error("expected next handler to be called when auth is disabled")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

func TestRequireWrite_NoSession_Returns401(t *testing.T) {
	authenticator := auth.New(auth.Config{
		GitHubClientID:     "test-client-id",
		GitHubClientSecret: "test-secret",
		AllowedTeams:       []string{"lineage/editors"},
		SessionKey:         make([]byte, 32),
		SecureCookies:      false,
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/sheets", nil)
	rec := httptest.NewRecorder()
	authenticator.RequireWrite(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogoutHandler_ClearsSession(t *testing.T) {
	authenticator := auth.New(auth.Config{
		GitHubClientID:     "test-client-id",
		GitHubClientSecret: "test-secret",
		AllowedTeams:       []string{"lineage/editors"},
		SessionKey:         make([]byte, 32),
		SecureCookies:      false,
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rec := httptest.NewRecorder()
	authenticator.LogoutHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("want 302 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("want redirect to /, got %q", loc)
	}
}

func TestLoginHandler_DisabledAuth_RedirectsHome(t *testing.T) {
	authenticator := auth.New(auth.Config{}) // disabled

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	authenticator.LoginHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("want 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("want redirect to /, got %q", loc)
	}
}

func TestLoginHandler_EnabledAuth_RedirectsToGitHub(t *testing.T) {
	authenticator := auth.New(auth.Config{
		GitHubClientID:     "test-client-id",
		GitHubClientSecret: "test-secret",
		AllowedTeams:       []string{"lineage/editors"},
		SessionKey:         make([]byte, 32),
		CallbackURL:        "https://maps.example.com/auth/callback",
		SecureCookies:      false,
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	authenticator.LoginHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("want 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" || loc == "/" {
		t.Errorf("want GitHub OAuth URL, got %q", loc)
	}
}
