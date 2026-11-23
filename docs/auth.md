# Authentication design

How write access is controlled and how the GitHub OAuth flow works.

## Who can do what

| Action | Requirement |
|---|---|
| `GET /sheets`, `GET /sheets/{id}` | None — public |
| `GET /iiif/…` (public sheet) | None by default; GitHub session or service credential when `READ_LOGIN_REQUIRED=true` |
| `GET /iiif/…` (private sheet) | GitHub session or service credential regardless of `READ_LOGIN_REQUIRED` |
| `POST /sheets` | GitHub session in an allowed team |
| `PATCH /sheets/{id}` | GitHub session in an allowed team |
| `DELETE /sheets/{id}` | GitHub session in an allowed team |
| `GET /` (upload form) | None — form is public; upload triggers auth check |
| `GET /manage` | GitHub session in an allowed team |

Write endpoints are gated by the `RequireWrite` middleware. IIIF endpoints are gated by
`RequireRead` when `READ_LOGIN_REQUIRED=true`; otherwise `RequireRead` is a passthrough.

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

`SESSION_MAX_AGE` is enforced at two levels: the browser cookie's `Max-Age`
attribute, and the signed session data itself. Both are set to the same value,
so a reissued cookie with a future expiry cannot extend a session beyond
`SESSION_MAX_AGE` seconds from the time of login.

## Team check

`GITHUB_ALLOWED_TEAMS` is a comma-separated list of `{org}/{team}` slugs.
GitHub converts team names to slugs automatically: spaces become hyphens and
everything is lowercased. For example, a team named "Map Editors" in the
`lineage` org has the slug `lineage/map-editors`.

To confirm the exact slug, open `github.com/orgs/{org}/teams/{slug}` — if the
page loads, the slug is correct.

Example value: `lineage/editors,lineage/admins`. On callback:

1. The server calls `GET https://api.github.com/user/teams?per_page=100` with
   the user's OAuth token, following `Link: rel="next"` pagination until all
   teams are fetched.
2. For each returned team it builds `{organization.login}/{team.slug}`.
3. If any entry is in `GITHUB_ALLOWED_TEAMS`, the user is granted access.
4. Otherwise `403 Forbidden` is returned and no session is created.

## Read login

By default IIIF reads are public. When `READ_LOGIN_REQUIRED=true`, every request
to `GET /iiif/…` must pass one of:

1. **GitHub session** — a cookie from a completed OAuth login (same session used
   for write access). The session must have `authenticated=true`.
2. **Service credential** — `Authorization: Bearer <SERVICE_CREDENTIAL>` header.
   The token is compared with `crypto/subtle.ConstantTimeCompare` to prevent
   timing attacks. This is intended for server-side callers such as the Allmaps
   Tile Server that cannot go through a browser login.

A request that passes neither receives `401 Unauthorized`.

When auth is entirely disabled (`GITHUB_CLIENT_ID` unset), `READ_LOGIN_REQUIRED`
has no effect — `RequireRead` is a passthrough regardless.

IIIF responses carry `Cache-Control: private` when read login is on, or when the
sheet itself is private (`public=false`). Otherwise they carry `Cache-Control: public`.

## Setting up the GitHub OAuth App

Create the OAuth App under the **organization** that owns the allowed teams, not
under a personal account. An org-owned app can always read that org's team
memberships; a personal app may be blocked if the org restricts third-party
access.

1. Go to **github.com/organizations/{org}/settings/applications** → OAuth Apps →
   **New OAuth App**.
2. Fill in the form:
   - **Application name** — something descriptive, e.g. `georef`
   - **Homepage URL** — your deployment URL
   - **Authorization callback URL** — `{BASE_URL}/auth/callback`, where
     `BASE_URL` is what you set in your environment, e.g.
     `https://maps.example.com/auth/callback`. The server builds the callback
     URL from `BASE_URL` automatically — there is no separate config var for it.
3. Click **Register application**.
4. Copy the **Client ID** → set as `GITHUB_CLIENT_ID`.
5. Click **Generate a new client secret** → copy immediately → set as
   `GITHUB_CLIENT_SECRET`.

Create a separate OAuth App for each environment (staging, production, local)
so callback URLs match exactly and secrets stay isolated.

### Generating SESSION_KEY

The session key is not from GitHub — generate it yourself:

```sh
openssl rand -base64 32
```

Set the output as `SESSION_KEY`. Keep it secret. Rotating it invalidates all
active sessions immediately (everyone is logged out).

## AUTH_REQUIRED vs READ_LOGIN_REQUIRED

These two flags are independent. It is a common mistake to conflate them.

| Flag | Controls | Default |
|---|---|---|
| `AUTH_REQUIRED` | Write access — `POST`, `PATCH`, `DELETE /sheets`, `/manage` | `true` |
| `READ_LOGIN_REQUIRED` | IIIF read access — `GET /iiif/…` for **all** sheets globally | `false` |

They can be set in any combination:

| `AUTH_REQUIRED` | `READ_LOGIN_REQUIRED` | Practical meaning |
|---|---|---|
| `true` | `false` | Writers must log in; anyone can view IIIF tiles (typical production) |
| `true` | `true` | Writers must log in; viewers must also authenticate or use a service credential |
| `false` | `false` | No auth anywhere — local development without GitHub configured |
| `false` | `true` | Writes are open but IIIF is gated — rarely useful |

A third layer of access control sits below these two flags: **per-sheet
visibility**. A sheet with `public=false` requires a valid session or service
credential on its IIIF endpoints even when `READ_LOGIN_REQUIRED=false`. This is
checked per request inside `resolveReadySheet` after the status check, not by a
middleware. `AUTH_REQUIRED=false` disables this check along with the rest of
auth.

The service credential (`SERVICE_CREDENTIAL`) is accepted by `RequireRead` and
the per-sheet private check, but **not** by `RequireWrite`. Machine-to-machine
callers (e.g. the Allmaps Tile Server) can read tiles without a browser session
but cannot upload or delete sheets.

## Configuration

| Env var | Required | Description |
|---|---|---|
| `AUTH_REQUIRED` | No | Set `false` to bypass all auth middleware even with GitHub configured; default `true` |
| `GITHUB_CLIENT_ID` | No — omit to disable auth | GitHub OAuth app client ID |
| `GITHUB_CLIENT_SECRET` | When auth enabled | GitHub OAuth app client secret |
| `GITHUB_ALLOWED_TEAMS` | When auth enabled | Comma-separated `org/team` slugs, e.g. `lineage/editors` |
| `SESSION_KEY` | When auth enabled | Base64-encoded signing key (≥ 32 bytes); generate with `openssl rand -base64 32` |
| `SESSION_MAX_AGE` | No | Session lifetime in seconds; default `604800` (7 days) |
| `SECURE_COOKIES` | No | Set `false` for local development over HTTP; default `true` |
| `READ_LOGIN_REQUIRED` | No | Set `true` to require auth on all IIIF endpoints globally; default `false`. Independent of `AUTH_REQUIRED` — see section above |
| `SERVICE_CREDENTIAL` | When `READ_LOGIN_REQUIRED=true` | Bearer token for server-side IIIF access |

The OAuth callback URL is not a separate variable — the server builds it as
`BASE_URL + /auth/callback`. Register that exact URL in the GitHub OAuth App.

None of these values belong in the repository. They are set in the deployment
environment only.

## Disabled mode

Auth is disabled in two cases:

- `GITHUB_CLIENT_ID` is not set, **or**
- `AUTH_REQUIRED=false`

When disabled:

- `RequireWrite` is a passthrough — all write requests succeed without a session.
- `RequireRead` is a passthrough — `READ_LOGIN_REQUIRED` has no effect.
- `/auth/login` and `/auth/callback` redirect immediately to `/`.
- `/auth/logout` clears any existing session and redirects to `/`.

`AUTH_REQUIRED=false` is useful when you want to keep all GitHub vars configured
(for a quick re-enable) but bypass the login requirement during local testing.

## Routes

| Method | Path | Auth required | Description |
|---|---|---|---|
| `GET` | `/auth/login` | No | Redirect to GitHub OAuth |
| `GET` | `/auth/callback` | No | Handle GitHub callback; create session |
| `POST` | `/auth/logout` | No | Clear session; redirect to `/` |
| `GET` | `/manage` | Write session | Sheet management page — list, visibility toggle |
