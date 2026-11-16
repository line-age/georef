package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

// stubTeamClient satisfies the internal githubTeamClient interface for testing.
type stubTeamClient struct {
	teams []githubTeam
	err   error
}

func (stub *stubTeamClient) listUserTeams(_ context.Context, _ *oauth2.Token) ([]githubTeam, error) {
	return stub.teams, stub.err
}

func TestCallbackHandler_NonAllowedTeam_Returns403(t *testing.T) {
	// Fake token endpoint that accepts any code and returns a bearer token.
	fakeGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "fake", "token_type": "bearer"}) //nolint:errcheck
	}))
	defer fakeGitHub.Close()

	cookieStore := sessions.NewCookieStore(make([]byte, 32))
	cookieStore.Options = &sessions.Options{Path: "/", MaxAge: 86400, HttpOnly: true, Secure: false}

	authenticator := &Authenticator{
		oauthConfig: &oauth2.Config{
			ClientID:     "id",
			ClientSecret: "secret",
			Endpoint:     oauth2.Endpoint{TokenURL: fakeGitHub.URL},
		},
		sessionStore: cookieStore,
		allowedTeams: map[string]bool{"lineage/editors": true},
		teamClient: &stubTeamClient{
			teams: []githubTeam{
				{Slug: "unrelated-team", Organization: githubOrganization{Login: "other-org"}},
			},
		},
	}

	// Use LoginHandler to generate a valid state and get the session cookie.
	loginReq := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	loginRec := httptest.NewRecorder()
	authenticator.LoginHandler(loginRec, loginReq)

	loginLocation, err := url.Parse(loginRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	state := loginLocation.Query().Get("state")

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/callback?code=fake&state="+state, nil)
	for _, cookie := range loginRec.Result().Cookies() {
		callbackReq.AddCookie(cookie)
	}
	callbackRec := httptest.NewRecorder()
	authenticator.CallbackHandler(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusForbidden {
		t.Errorf("want 403 Forbidden, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}
}

func TestParseLinkNext(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "empty header",
			header: "",
			want:   "",
		},
		{
			name:   "single next link",
			header: `<https://api.github.com/user/teams?page=2&per_page=100>; rel="next"`,
			want:   "https://api.github.com/user/teams?page=2&per_page=100",
		},
		{
			name:   "next and last links",
			header: `<https://api.github.com/user/teams?page=2>; rel="next", <https://api.github.com/user/teams?page=5>; rel="last"`,
			want:   "https://api.github.com/user/teams?page=2",
		},
		{
			name:   "last page - no next",
			header: `<https://api.github.com/user/teams?page=4>; rel="prev", <https://api.github.com/user/teams?page=5>; rel="last"`,
			want:   "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseLinkNext(testCase.header)
			if got != testCase.want {
				t.Errorf("parseLinkNext(%q) = %q, want %q", testCase.header, got, testCase.want)
			}
		})
	}
}
