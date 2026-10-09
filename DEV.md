# Development notes

This document describes how the project is developed and run locally, and how this differs from production deployment.

## Development mode

In development mode, the frontend and backend are run separately to maximize iteration speed and debuggability.

- The frontend is served by Vite using a Node.js development server.
- ES modules are loaded directly without bundling.
- Hot module reload is enabled.
- Plugins are loaded dynamically as separate modules.
- The frontend communicates with the backend over HTTP (REST).

In this mode, the Go backend:
- exposes only API endpoints (documents, media, search, export, etc.),
- does not serve frontend assets.

This mode is intended for:
- frontend and plugin development,
- rapid iteration,
- debugging document semantics and editor behavior.

## Production mode

In production mode, the frontend is built ahead of time and served as static assets.

- The frontend is bundled into standalone JavaScript and CSS files.
- The core frontend and each plugin are built as separate bundles.
- No Node.js runtime or build tools are required at runtime.

In this mode, the Go backend:
- serves the bundled frontend assets (either from disk or embedded resources),
- exposes the same REST API as in development mode.

This mode is intended for:
- deployment,
- stability,
- minimal runtime dependencies.

## Bundling strategy

The frontend build produces:
- a core bundle containing the editor, registry, and core document semantics,
- one bundle per plugin.

Plugins are loaded dynamically by the frontend at runtime. Enabling or disabling a plugin does not require rebuilding the core bundle.

## Invariants

The following invariants apply in all modes:

- The Go backend never depends on Node.js, Vite, or the TypeScript toolchain.
- The backend is authoritative for storage, identity, access control, and export.
- The frontend is responsible for document semantics, editing, and rendering.
- The communication boundary between frontend and backend is HTTP (REST).

# Tips

## Manual user management

⏺ The auth system uses `data/meta/users.json`. On first startup, it auto-creates this file with a default admin/admin account (and logs a warning).                                                                                                 
                                                                                                                           
  To add or change users, edit that file directly. The format is:                 

```json                                                                                                                
  [                                                                                                              
    {"username": "admin", "password_hash": "$2a$10$..."},
    {"username": "alice", "password_hash": "$2a$10$..."}
  ]
```

  To generate a bcrypt hash for a new password, you can use:

  ### With htpasswd (if installed)
  htpasswd -nbBC 10 "" "yourpassword" | cut -d: -f2

  ### Or with Go one-liner
  go run -e 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { h, _ :=
  bcrypt.GenerateFromPassword([]byte("yourpassword"), bcrypt.DefaultCost); fmt.Println(string(h)) }'

  ### Or with Python
  python3 -c "import bcrypt; print(bcrypt.hashpw(b'yourpassword', bcrypt.gensalt()).decode())"

## Version bump

Versioning follows semver with release candidates (`1.0.0-rc.N`). The RC
rule from `CLAUDE.md`: bug fixes land during the RC window without
changing the version; feature additions push the RC number
(`rc.N → rc.N+1`) rather than the base version. A clean RC that ages a
week with no reroll becomes the base version tag unchanged.

### Procedure

1. **Pick the new version.** Any new user-visible attribute, directive,
   endpoint, or default-behaviour change bumps the RC number. Pure
   fixes and refactors do not — they ship under the current tag's
   build date.

2. **Scan commits since the previous tag.**
   ```
   git log --oneline $(git describe --tags --abbrev=0)..HEAD
   ```
   Group by **Added** (new surface the user can call out), **Fixed**
   (bugs resolved), and **Internal** (plumbing, reconcilers, tests).
   Mid-RC fixes that corrected prior commits in the same RC window
   belong in **Fixed** too, framed as "shipped as a mid-rc fix".

3. **Update the version constant.**
   ```
   backend/internal/api/server.go
       const Version = "1.0.0-rc.N"
   ```
   The `/api/version` endpoint reads this constant, so prod verification
   compares `/api/version` → `commit` to `git rev-parse HEAD` AND
   `version` to this constant.

4. **Write the CHANGELOG entry.** In `CHANGELOG.md`, replace the
   `[Unreleased]` body with `Nothing pending — the working tree
   matches 'v1.0.0-rc.N'.`, then insert a new section header above the
   previous release:
   ```
   ## [1.0.0-rc.N] — YYYY-MM-DD
   ```
   Match the existing entries' style: one-paragraph lead summarising
   the RC's shape (why it exists), then **Added**, **Fixed**,
   **Internal** sections. Entries are prose, not bullet-list dumps —
   each one names the user-visible symptom (or the regulatory
   consequence for QMS-adjacent bugs), the root cause in one phrase,
   and the fix in one phrase. Keep file/line citations out of the
   changelog; the commit log is for that.

5. **Commit, tag, push to both remotes.**
   ```
   git add backend/internal/api/server.go CHANGELOG.md
   git commit -m "Release: v1.0.0-rc.N
   <two-paragraph summary matching the CHANGELOG lead>"
   git tag v1.0.0-rc.N
   git push origin main
   git push origin v1.0.0-rc.N
   git push gmt main
   git push gmt v1.0.0-rc.N
   ```
   The `gmt` remote is the regulatory continuity mirror — every tag
   goes there as well as to `origin`.

6. **Build + deploy** the stamped binary and (if the frontend changed)
   the frontend bundle. See the deploy procedure in session memory;
   the ldflags that stamp `BuildCommit` and `BuildDate` are
   non-optional — a build without them leaves `/api/version` returning
   `"unknown"`, which breaks the deploy-vs-source audit path.

7. **Verify** `/api/version` on prod returns the just-tagged commit SHA
   and the new version string. If it still shows the previous version
   you shipped a non-stamped build; rebuild with `-ldflags` and
   redeploy.

### Base-version tagging (rc → final)

When an RC has aged without a reroll and you want to tag the base
version:

- Confirm no fix in the RC window was marked as "shipped as a mid-rc
  fix" more than once (if it was, that's a reroll — bump the RC
  instead).
- Tag `v1.0.0` on the same commit the RC points to (no code change
  needed) and push both remotes.
- CHANGELOG: insert `## [1.0.0] — YYYY-MM-DD` with a one-line body
  pointing at the equivalent RC (`Equivalent to v1.0.0-rc.N; see its
  entry for the content.`). Do not duplicate the RC's prose.