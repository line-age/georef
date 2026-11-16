# Authentication design

How write access is controlled and how the GitHub OAuth flow works.

## Who can do what

| Action | Requirement |
|---|---|
| `GET /sheets`, `GET /sheets/{id}` | None — public |
| `GET /iiif/…` | None by default; optional read gate in Stage 11 |
| `POST /sheets` | GitHub session in an allowed team |
| `PATCH /sheets/{id}` | GitHub session in an allowed team |
| `DELETE /sheets/{id}` | GitHub session in an allowed team |
| `GET /` (upload form) | None — form is public; upload triggers auth check |

Write endpoints are gated by the `RequireWrite` middleware. Reads are open.

## GitHub OAuth flow

Authentication uses GitHub's OAuth 2.0 device flow with cookie sessions.

```
GET /auth/login
  → 302 to github.com/login/oauth/authorize?client_id=…&state=…&scope=read:org
  → user approves on GitHub
  → GET /auth/callback?code=…&state=…
  → server exchanges code for token
  → server calls GET https://api.github.com/user/teams (paginated, all results)
  → if any team slug matches GITHUB_ALLOWED_TEAMS → set session, 302 /
  → otherwise → 403 Forbidden
```

The `state` parameter is a 16-byte random nonce stored in the session cookie.
The callback validates it before exchanging the code, preventing CSRF.

## Session

Sessions are signed cookie-backed (`gorilla/sessions` with `gorilla/securecookie`).
No session data is stored server-side; the signed cookie is the session.

| Field | Value |
|---|---|
| Cookie name | `georef` |
| Lifetime | `SESSION_MAX_AGE` (default 7 days) |
| Signing key | `SESSION_KEY` (base64, ≥ 32 bytes) |
| `HttpOnly` | yes — not readable by JavaScript |
| `Secure` | yes by default; set `SECURE_COOKIES=false` for local HTTP |
| `SameSite` | Lax |

The session stores only `authenticated=true`. The token is not persisted;
team membership is re-checked at next login after session expiry.

## Team check

`GITHUB_ALLOWED_TEAMS` is a comma-separated list of `{org}/{team}` slugs,
for example `lineage/editors,lineage/admins`. On callback:

1. The server calls `GET https://api.github.com/user/teams?per_page=100` with
   the user's OAuth token, following `Link: rel="next"` pagination until all
   teams are fetched.
2. For each returned team it builds `{organization.login}/{team.slug}`.
3. If any entry is in `GITHUB_ALLOWED_TEAMS`, the user is granted access.
4. Otherwise `403 Forbidden` is returned and no session is created.

## Configuration

| Env var | Required | Description |
|---|---|---|
| `GITHUB_CLIENT_ID` | No — omit to disable auth | GitHub OAuth app client ID |
| `GITHUB_CLIENT_SECRET` | When auth enabled | GitHub OAuth app client secret |
| `GITHUB_ALLOWED_TEAMS` | When auth enabled | Comma-separated `org/team` pairs |
| `SESSION_KEY` | When auth enabled | Base64-encoded signing key (≥ 32 bytes) |
| `SESSION_MAX_AGE` | No | Session lifetime in seconds; default `604800` (7 days) |
| `SECURE_COOKIES` | No | Set `false` for local development over HTTP; default `true` |

None of these values belong in the repository. They are set in the deployment
environment only.

## Disabled mode

When `GITHUB_CLIENT_ID` is not set:

- `RequireWrite` is a passthrough — all write requests succeed without a session.
- `/auth/login` and `/auth/callback` redirect immediately to `/`.
- `/auth/logout` clears any existing session and redirects to `/`.

This makes local development work without registering a GitHub OAuth app.

## Routes

| Method | Path | Auth required | Description |
|---|---|---|---|
| `GET` | `/auth/login` | No | Redirect to GitHub OAuth |
| `GET` | `/auth/callback` | No | Handle GitHub callback; create session |
| `POST` | `/auth/logout` | No | Clear session; redirect to `/` |
