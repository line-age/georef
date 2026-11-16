// Package auth implements GitHub OAuth2, cookie sessions, and team-membership middleware.
// Write endpoints require a valid session in an allowed GitHub team.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

const (
	sessionName          = "georef"
	sessionKeyAuth       = "authenticated"
	sessionKeyOAuthState = "oauth_state"
	defaultSessionMaxAge = 86400 * 7 // 7 days

	msgUnauthorized        = "authentication required"
	msgForbidden           = "not a member of an allowed team"
	msgStateMismatch       = "invalid oauth state"
	msgTokenExchangeFailed = "token exchange failed"
	msgTeamCheckFailed     = "team membership check failed"

	githubTeamsURL = "https://api.github.com/user/teams?per_page=100"
)

// Config holds the authentication configuration.
type Config struct {
	GitHubClientID     string
	GitHubClientSecret string
	AllowedTeams       []string // "org/team" pairs, e.g. ["lineage/editors"]
	SessionKey         []byte   // signing key for session cookies; at least 32 bytes
	CallbackURL        string   // e.g. "https://maps.example.com/auth/callback"
	SecureCookies      bool     // set false only in local HTTP development
	SessionMaxAge      int      // session lifetime in seconds; 0 means use defaultSessionMaxAge (7 days)
}

// Authenticator handles the GitHub OAuth2 flow, session cookies, and the write-auth middleware.
// A nil or disabled Authenticator is a passthrough: RequireWrite allows all requests.
type Authenticator struct {
	disabled     bool
	oauthConfig  *oauth2.Config
	sessionStore sessions.Store
	allowedTeams map[string]bool
	teamClient   githubTeamClient
}

// githubTeamClient fetches the teams an OAuth token holder belongs to.
// The interface allows the real HTTP call to be replaced in tests.
type githubTeamClient interface {
	listUserTeams(ctx context.Context, token *oauth2.Token) ([]githubTeam, error)
}

// githubTeam is the relevant subset of a GitHub team response entry.
type githubTeam struct {
	Slug         string             `json:"slug"`
	Organization githubOrganization `json:"organization"`
}

type githubOrganization struct {
	Login string `json:"login"`
}

// New returns an Authenticator configured from cfg.
// When GitHubClientID is empty, authentication is disabled and RequireWrite is a no-op.
func New(cfg Config) *Authenticator {
	if cfg.GitHubClientID == "" {
		return &Authenticator{disabled: true}
	}

	allowedTeams := make(map[string]bool, len(cfg.AllowedTeams))
	for _, team := range cfg.AllowedTeams {
		allowedTeams[team] = true
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.GitHubClientID,
		ClientSecret: cfg.GitHubClientSecret,
		Scopes:       []string{"read:org"},
		Endpoint:     github.Endpoint,
		RedirectURL:  cfg.CallbackURL,
	}

	sessionMaxAge := cfg.SessionMaxAge
	if sessionMaxAge <= 0 {
		sessionMaxAge = defaultSessionMaxAge
	}

	cookieStore := sessions.NewCookieStore(cfg.SessionKey)
	cookieStore.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	}

	return &Authenticator{
		oauthConfig:  oauthCfg,
		sessionStore: cookieStore,
		allowedTeams: allowedTeams,
		teamClient:   &httpTeamClient{oauthConfig: oauthCfg},
	}
}

// LoginHandler redirects the browser to GitHub to begin the OAuth2 authorisation flow.
func (authenticator *Authenticator) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if authenticator.disabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	state := randomState()
	session, _ := authenticator.sessionStore.Get(r, sessionName)
	session.Values[sessionKeyOAuthState] = state
	if err := session.Save(r, w); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, authenticator.oauthConfig.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler handles the GitHub OAuth2 callback.
// On success: sets an authenticated session and redirects to /.
// On team mismatch: returns 403 without creating a session.
func (authenticator *Authenticator) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	if authenticator.disabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	session, _ := authenticator.sessionStore.Get(r, sessionName)

	expected, _ := session.Values[sessionKeyOAuthState].(string)
	if expected == "" || r.URL.Query().Get("state") != expected {
		http.Error(w, msgStateMismatch, http.StatusBadRequest)
		return
	}

	// Consume the state immediately regardless of outcome to prevent replay attacks.
	delete(session.Values, sessionKeyOAuthState)
	if err := session.Save(r, w); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	token, err := authenticator.oauthConfig.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, msgTokenExchangeFailed, http.StatusInternalServerError)
		return
	}

	teams, err := authenticator.teamClient.listUserTeams(r.Context(), token)
	if err != nil {
		http.Error(w, msgTeamCheckFailed, http.StatusInternalServerError)
		return
	}

	for _, team := range teams {
		slug := team.Organization.Login + "/" + team.Slug
		if authenticator.allowedTeams[slug] {
			session.Values[sessionKeyAuth] = true
			session.Save(r, w) //nolint:errcheck
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	http.Error(w, msgForbidden, http.StatusForbidden)
}

// LogoutHandler clears the session and redirects to /.
func (authenticator *Authenticator) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if authenticator.disabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	session, _ := authenticator.sessionStore.Get(r, sessionName)
	session.Options.MaxAge = -1
	session.Save(r, w) //nolint:errcheck
	http.Redirect(w, r, "/", http.StatusFound)
}

// RequireWrite is middleware that rejects unauthenticated write requests.
// Returns 401 when no authenticated session exists.
// When auth is disabled, all requests pass through.
func (authenticator *Authenticator) RequireWrite(next http.Handler) http.Handler {
	if authenticator.disabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := authenticator.sessionStore.Get(r, sessionName)
		if err != nil || session.IsNew {
			writeAuthError(w, http.StatusUnauthorized, msgUnauthorized)
			return
		}
		if authenticated, ok := session.Values[sessionKeyAuth].(bool); !ok || !authenticated {
			writeAuthError(w, http.StatusUnauthorized, msgUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// httpTeamClient fetches GitHub team memberships via the real GitHub API.
type httpTeamClient struct {
	oauthConfig *oauth2.Config
}

func (client *httpTeamClient) listUserTeams(ctx context.Context, token *oauth2.Token) ([]githubTeam, error) {
	httpClient := client.oauthConfig.Client(ctx, token)

	var allTeams []githubTeam
	nextURL := githubTeamsURL
	for nextURL != "" {
		page, next, err := client.fetchTeamsPage(ctx, httpClient, nextURL)
		if err != nil {
			return nil, err
		}
		allTeams = append(allTeams, page...)
		nextURL = next
	}
	return allTeams, nil
}

func (client *httpTeamClient) fetchTeamsPage(ctx context.Context, httpClient *http.Client, pageURL string) ([]githubTeam, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	var teams []githubTeam
	if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
		return nil, "", err
	}
	return teams, parseLinkNext(resp.Header.Get("Link")), nil
}

// parseLinkNext extracts the "next" URL from a GitHub API Link header.
// GitHub format: Link: <https://...>; rel="next", <https://...>; rel="last"
func parseLinkNext(linkHeader string) string {
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		sections := strings.SplitN(part, ";", 2)
		if len(sections) != 2 {
			continue
		}
		urlPart := strings.TrimSpace(sections[0])
		relPart := strings.TrimSpace(sections[1])
		if relPart == `rel="next"` && len(urlPart) > 2 && urlPart[0] == '<' && urlPart[len(urlPart)-1] == '>' {
			return urlPart[1 : len(urlPart)-1]
		}
	}
	return ""
}

// randomState generates a cryptographically random state string for the OAuth2 flow.
func randomState() string {
	nonce := make([]byte, 16)
	rand.Read(nonce) //nolint:errcheck
	return base64.URLEncoding.EncodeToString(nonce)
}

// writeAuthError writes a JSON error response with the given status and message.
func writeAuthError(w http.ResponseWriter, status int, msg string) {
	body, _ := json.Marshal(map[string]string{"error": msg})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body) //nolint:errcheck
}
