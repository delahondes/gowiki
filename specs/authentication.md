# Authentication & Authorization — Specification

## Overview

Gowiki supports two authentication methods: local username/password and OAuth/OIDC with Azure AD (Microsoft 365). Authorization is enforced via a regex-based ACL system. All auth state is stored as JSON files in `data/meta/`.

## Data Model

### User

```json
{
  "username": "alice",
  "password_hash": "$2a$10$...",
  "email": "alice@example.com",
  "display_name": "Alice Martin",
  "groups": ["editors"],
  "oauth_groups": ["quality-team"],
  "disabled": false,
  "created_at": "2025-01-15T10:00:00Z",
  "last_login": "2026-03-14T08:30:00Z"
}
```

- `password_hash`: bcrypt (`$2a$`). Empty string for OAuth-only users (no local login).
- `groups`: locally-assigned groups, managed by admins.
- `oauth_groups`: synced from external provider on each OAuth login. Never modified by admins.
- `disabled`: if true, both local and OAuth login are rejected.

**Effective groups** = union of `groups` and `oauth_groups`. Used for ACL evaluation and admin checks.

Storage: `data/meta/users.json` (JSON array).

### Group

```json
{
  "name": "editors",
  "description": "Can edit all pages"
}
```

Groups are labels referenced by ACL rules. Two groups are always bootstrapped: `admin` (administrators) and `editors`.

Storage: `data/meta/groups.json` (JSON array).

### Session

```json
{
  "<session_id>": {
    "username": "alice",
    "expiry": "2026-03-15T08:30:00Z"
  }
}
```

- `session_id`: 32 random bytes, hex-encoded (64 chars).
- `expiry`: absolute timestamp. Sessions are purged after expiry.

Storage: `data/meta/sessions.json` (JSON object keyed by session ID).

### ACL Rule

```json
{
  "pattern": "regulatory/.*",
  "subject_type": "group",
  "subject": "quality-team",
  "permissions": ["view", "edit"]
}
```

- `pattern`: Go regexp matched against the page path (anchored: `^(?:pattern)$`). May contain `@self` placeholder for `@self` rules.
- `subject_type`: `"user"`, `"group"`, or `"special"`.
- `subject`: username, group name, `"@all"` (everyone including anonymous), `"@authenticated"` (any logged-in user), or `"@self"` (per-user, with `@self` in pattern).
- `permissions`: subset of `["view", "edit", "delete"]`. Empty array = deny all.

Storage: `data/meta/acl.json` (JSON array).

## Local Authentication

### Login flow

1. Frontend sends `POST /api/auth/login` with `{ "username", "password" }`.
2. Backend looks up user by username, checks `disabled` flag, then compares password against `password_hash` using bcrypt.
3. On success: records `last_login`, creates a session, sets the session cookie, returns `{ "username" }`.
4. On failure: returns `401 invalid credentials` or `403 account is disabled`.

### Session cookie

- Name: `gowiki_session`
- Path: `/`
- Flags: `HttpOnly`, `SameSite=Lax`
- `MaxAge`: matches session TTL (default 24h, configurable via `auth.session_ttl` in `data/config.yaml`)
- No `Secure` flag (to support HTTP dev environments). Should be set behind an HTTPS reverse proxy via `Set-Cookie` header rewriting.

### Session lifecycle

- **Creation**: on successful login (local or OAuth). Generates 32 random bytes, stores in `sessions.json`.
- **Validation**: on each request, middleware reads the cookie, looks up the session, checks expiry.
- **Expiry**: if `time.Now() > session.Expiry`, the session is deleted and the cookie cleared.
- **Logout**: `POST /api/auth/logout` deletes the session and clears the cookie.
- **Persistence**: sessions survive server restarts. `sessions.json` is written atomically (temp file + rename) on every create/delete.
- **Cleanup**: a background goroutine runs every 15 minutes to purge expired sessions and save to disk.

### Session TTL configuration

```yaml
# data/config.yaml
auth:
  session_ttl: "24h"    # Go duration string
```

Configurable via the admin UI. Applies to new sessions only — existing sessions keep their original expiry. Default: 24 hours.

### Bootstrap

On first start with no `users.json`, the system creates a default `admin` user with password `admin` and group `["admin"]`. A log warning is emitted.

## OAuth / OIDC (Azure AD)

### Configuration

```yaml
# data/config.yaml
auth:
  oauth:
    provider: "azure"
    tenant_id: "<Azure AD tenant ID>"
    client_id: "<Application (client) ID>"
    client_secret: "<Client secret value>"
    auto_create_users: true
    default_groups: ["editors"]
```

- `provider`: only `"azure"` is supported. Empty or absent = OAuth disabled.
- `auto_create_users`: if true, users logging in via OAuth for the first time are automatically created.
- `default_groups`: local groups assigned to auto-created OAuth users.

### Provider discovery

On startup (or first use), the backend performs OIDC discovery against `https://login.microsoftonline.com/{tenant_id}/v2.0`. This fetches the authorization, token, and JWKS endpoints.

### OAuth scopes

`openid`, `email`, `profile`, `GroupMember.Read.All`

### Login flow

1. Frontend navigates to `GET /api/auth/oauth/login?return_to=/current/page`.
2. Backend generates a random CSRF state token (16 bytes, hex-encoded), stores it in an in-memory map along with the user's origin URL.
3. Backend builds the authorization URL with the callback set to `{origin}/api/auth/oauth/callback`, then redirects (302) the browser to Azure.
4. User authenticates with Microsoft. Azure redirects back to `GET /api/auth/oauth/callback?code=...&state=...`.
5. Backend verifies the state token (CSRF protection), then exchanges the authorization code for tokens.
6. Backend verifies the ID token signature via OIDC JWKS.
7. Backend extracts claims (`email`, `name`, `preferred_username`, `upn`). Email is resolved from `email` → `preferred_username` → `upn` in priority order.
8. If the access token is present, backend calls Microsoft Graph API (`GET /v1.0/me/memberOf`) to fetch the user's Azure AD group memberships. Group display names are lowercased.
9. Azure groups that don't exist in the group store are auto-created with description "Imported from Azure AD".

### User matching and creation

- Users are matched by **email** (case-insensitive).
- If no user exists and `auto_create_users` is true:
  - Username derived from email prefix (part before `@`), sanitized to `[a-z0-9_.-]`. If taken, `_oauth` is appended.
  - Display name from the `name` claim, falling back to email.
  - Local groups set to `default_groups` from config.
  - OAuth groups set from the Azure AD group fetch.
- If no user exists and `auto_create_users` is false: login is rejected with an error page.
- If the user exists: only `oauth_groups` are updated. Local `groups` are never modified by OAuth login.
- Disabled users are rejected regardless of OAuth success.

### Callback URL handling

The OAuth callback URL is dynamically computed from the incoming request to support multiple access methods (direct, Vite dev proxy, nginx reverse proxy). The origin is resolved in this priority:

1. `X-Forwarded-Host` + `X-Forwarded-Proto` headers (reverse proxy)
2. `Origin` header (browser navigation)
3. `Referer` header (extract scheme://host)
4. `r.Host` with TLS-based scheme detection (fallback)

### Error handling

OAuth errors (denied consent, invalid code, missing email claim) render an HTML error page since the flow is a browser redirect — not a JSON API call. The page includes a link back to the wiki root.

### Auth providers endpoint

`GET /api/auth/providers` returns which login methods are available:

```json
{
  "local": true,
  "providers": [
    { "name": "azure", "label": "Microsoft 365" }
  ]
}
```

The `providers` array is empty when OAuth is not configured. The frontend uses this to decide whether to show the "Sign in with Microsoft" button.

## ACL Evaluation

### Algorithm

1. Collect all rules whose `pattern` regexp matches the page path. For `@self` rules, `@self` in the pattern is replaced with the authenticated username (regex-escaped) before matching.
2. Filter to rules whose subject applies to the current user:
   - `"special" / "@all"`: always matches.
   - `"special" / "@authenticated"`: matches if the user is logged in.
   - `"special" / "@self"`: matches if the user is logged in (pattern already filtered by username via `@self` substitution).
   - `"user"`: matches if `subject == username`.
   - `"group"`: matches if the subject is in the user's effective groups.
3. Sort matches by pattern length (descending) as a proxy for specificity.
4. Take the most specific tier (all rules sharing the longest pattern length).
5. Union permissions from all rules in that tier — most permissive wins within a specificity level.
6. Check if the requested action is in the resulting permission set.
7. If no rules match at all: **deny by default**.

### Middleware integration

Three middleware layers enforce auth/ACL:

| Middleware | Purpose | Used by |
|---|---|---|
| `optionalAuth` | Reads session cookie if present, sets username in context. Does not reject anonymous requests. | Read endpoints, search, public data |
| `requireAuth` | Rejects unauthenticated requests with 401. Sets username in context. | Write endpoints, delete endpoints |
| `requireAdmin` | Requires `requireAuth` + user must belong to the `admin` group. | Admin API endpoints |

ACL checks (`aclStore.CheckPermission`) are called within individual handlers, not as middleware, because the page path is needed and is route-dependent.

### Per-user rules (`@self`)

The special subject `@self` allows ACL rules where each authenticated user gets permissions on pages that match their own username. The pattern uses `@self` as a placeholder that is substituted with the requesting user's username at evaluation time.

```json
{
  "pattern": "staff/@self/.*",
  "subject_type": "special",
  "subject": "@self",
  "permissions": ["view", "edit", "delete"]
}
```

This gives each user full control over their own `staff/<username>/` namespace. For example, user `alice` can edit `staff/alice/notes` but not `staff/bob/notes`.

The `@self` placeholder is regex-escaped at evaluation time, so usernames with special characters (`.`, `+`, etc.) are matched literally.

Typical use cases:
- Personal namespaces: `staff/@self/.*` — each user owns their folder
- Per-user documents: `interviews/@self-.*` — pages prefixed with the username (e.g., `alice-2024`)
- Anonymous users never match `@self` rules

This is the Gowiki equivalent of DokuWiki's `%USER%` ACL template rules.

### Default ACL rules

When `acl.json` does not exist, these bootstrap rules are created:

| Pattern | Subject | Permissions |
|---|---|---|
| `.*` | group `admin` | view, edit, delete |
| `.*` | group `editors` | view, edit |
| `.*` | special `@all` | view |

This makes the wiki readable by everyone and editable by the `editors` group out of the box.

## API Reference

### Public endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/auth/login` | Local login (username + password) |
| `POST` | `/api/auth/logout` | Destroy session |
| `GET` | `/api/auth/me` | Current user info (username, display_name, email, is_admin) |
| `GET` | `/api/auth/providers` | Available login methods |
| `GET` | `/api/auth/oauth/login` | Start OAuth flow (redirects to Azure) |
| `GET` | `/api/auth/oauth/callback` | OAuth callback (exchanges code for session) |

### Admin endpoints (require `admin` group)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/admin/users` | List all users (password hashes stripped) |
| `POST` | `/api/admin/users` | Create user (username, password, email, display_name, groups) |
| `PUT` | `/api/admin/users/{username}` | Update user (email, display_name, groups, disabled) |
| `DELETE` | `/api/admin/users/{username}` | Delete user (self-deletion prevented) |
| `PUT` | `/api/admin/users/{username}/password` | Set user password |
| `GET` | `/api/admin/groups` | List all groups |
| `POST` | `/api/admin/groups` | Create group (name, description) |
| `PUT` | `/api/admin/groups/{name}` | Update group description |
| `DELETE` | `/api/admin/groups/{name}` | Delete group (does not auto-remove from users) |
| `GET` | `/api/admin/acl` | List ACL rules |
| `PUT` | `/api/admin/acl` | Replace entire ACL ruleset (validated before saving) |

### Semi-public endpoints (optional auth)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/users/display?users=alice,bob` | Display info for usernames (respects `site.user_display` config) |
| `GET` | `/api/users/list` | All active users with username and display name |

## Storage Files

| File | Format | Description |
|---|---|---|
| `data/meta/users.json` | JSON array of User | All user accounts |
| `data/meta/groups.json` | JSON array of Group | All groups |
| `data/meta/sessions.json` | JSON object `{id: Session}` | Active sessions |
| `data/meta/acl.json` | JSON array of ACLRule | Access control rules |
| `data/config.yaml` | YAML | Auth config under `auth:` key |

All stores use atomic writes (temp file + rename) to prevent corruption on crash.

## DokuWiki Import

The importer (`backend/cmd/import/`) can import DokuWiki users and ACLs:

- **Users**: parsed from `conf/users.auth.php`. Bcrypt `$2y$` hashes are converted to `$2a$` (same algorithm, Go-compatible prefix). Non-bcrypt hashes (MD5, phpass) are discarded — those users must reset their password.
- **ACL rules**: parsed from `conf/acl.auth.php`. DokuWiki colon paths are converted to regex patterns. Numeric permission levels are mapped: 1→view, 2→view+edit, 16+→view+edit+delete. DokuWiki's `%USER%` template rules are converted to `@self` rules.
- **`--fallback-admin`**: creates an admin/admin account with full `.*` permissions when no users exist in the source.
