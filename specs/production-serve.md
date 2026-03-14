# Production Serving

## Overview

Gowiki runs as a single self-contained binary in production. The Go backend serves both the API and the bundled frontend assets, handles TLS termination via Let's Encrypt, and redirects HTTP to HTTPS. No reverse proxy (nginx, Caddy) is needed.

## Architecture

```
Internet
   |
   +- :443 (HTTPS) --> Go server (TLS via autocert)
   |                      +-- /api/*        -> REST handlers
   |                      +-- /icons/*      -> static assets
   |                      +-- /*            -> SPA fallback (index.html)
   |
   +- :80  (HTTP)  --> redirect -> https://{domain}/...
```

The server uses Go's `crypto/tls` with `golang.org/x/crypto/acme/autocert` for automatic certificate provisioning and renewal from Let's Encrypt.

## Configuration

All settings can be specified in a YAML config file, passed via the `-config` CLI flag. CLI flags override config file values when both are provided.

### Config file

```yaml
# /opt/gowiki/config.yaml

data_dir: /opt/gowiki/data          # root data directory (contains content/, meta/, attic/, certs/)

server:
  addr: ":8080"                      # HTTP listen address (ignored when tls_domain is set)
  tls_domain: "wiki.example.com"     # enables HTTPS on :443, HTTP redirect on :80
  web_dir: "/opt/gowiki/frontend/dist"  # built frontend assets (implies -serve-web)

site:
  title: "My Wiki"
  base_url: "https://wiki.example.com"
  # ...

database:
  enabled: true
  dsn: "postgres://gowiki:pass@localhost/gowiki?sslmode=disable"

# ... (auth, drafts, todo, reviewflow sections as before)
```

When `server.web_dir` is set in the config, the server automatically serves frontend assets (equivalent to `-serve-web`).

### Data directory layout

The `data_dir` path (from config or `-data-dir` flag) is the root. All subdirectories are derived:

```
data_dir/
  content/     pages (.md) and media attachments
  meta/        metadata, indexes, auth stores, sessions
  attic/       version history archive
  drafts/      in-progress edits
  certs/       TLS certificates (auto-created when tls_domain is set)
  config.yaml  site config (legacy location, used when -config is not given)
```

### Command-line flags

All flags are optional. They override config file values.

| Flag | Description |
|------|-------------|
| `-config` | Path to config file (YAML). When omitted, loads from `{data_dir}/config.yaml`. |
| `-data-dir` | Data root directory. Overrides `data_dir` in config. Default: `./backend/data`. |
| `-addr` | HTTP listen address. Overrides `server.addr` in config. |
| `-serve-web` | Serve built frontend assets. |
| `-web-dir` | Frontend assets directory. Overrides `server.web_dir` in config. |
| `-tls-domain` | Domain for Let's Encrypt auto-TLS. Overrides `server.tls_domain` in config. |

### Resolution order

For each setting, the effective value is determined by (highest priority first):
1. CLI flag (if explicitly set)
2. Config file value
3. Built-in default

## Modes of operation

### Development (no config file)

```bash
make dev
```

No config file needed. Uses defaults: `-data-dir ./backend/data`, plain HTTP on `:8080`.

### Production with config file (recommended)

```bash
gowiki-server -config /opt/gowiki/config.yaml
```

Everything is in the config file. The CLI is just the binary path and the config path.

### Production without TLS

```bash
gowiki-server -config /opt/gowiki/config.yaml
```

With config:
```yaml
data_dir: /opt/gowiki/data
server:
  addr: ":8080"
  web_dir: "/opt/gowiki/frontend/dist"
```

Plain HTTP. Useful behind a separate TLS terminator or on a private network.

### Production with TLS

```yaml
data_dir: /opt/gowiki/data
server:
  tls_domain: "wiki.example.com"
  web_dir: "/opt/gowiki/frontend/dist"
```

When `tls_domain` is set:

1. The server listens on **:443** for HTTPS with an automatically provisioned Let's Encrypt certificate.
2. A background HTTP server listens on **:80** for ACME HTTP-01 challenges and HTTP-to-HTTPS redirect.
3. `server.addr` is ignored.
4. Certificates are cached in `{data_dir}/certs/` (created automatically).

## Certificate management

- **Provisioning**: Automatic via Let's Encrypt ACME HTTP-01 challenge on first request.
- **Renewal**: Automatic -- `autocert` renews certificates ~30 days before expiry.
- **Cache**: Stored in `{data_dir}/certs/` using `autocert.DirCache`. Must be persistent across restarts.
- **Domain validation**: Only the exact domain specified in `tls_domain` is accepted.

## Firewall requirements

When using `tls_domain`:
- Port **80** must be reachable from the internet (ACME challenges + HTTP redirect).
- Port **443** must be reachable from the internet (HTTPS traffic).

## Graceful shutdown

The server handles `SIGINT` and `SIGTERM` signals:

1. Stops accepting new connections.
2. Waits up to **10 seconds** for in-flight requests to complete.
3. Exits cleanly.

## pprof

The debug profiling server (`:6060`) is only started when `tls_domain` is **not** set. In TLS mode, pprof is disabled to avoid exposing debug endpoints on a public server.

## Deployment example

### Minimal VPS setup (Ubuntu/Debian)

```bash
# 1. Build
make build-frontend
cd backend && GOOS=linux GOARCH=amd64 go build -o gowiki-server ./cmd/server && cd ..

# 2. Copy to server
scp backend/gowiki-server user@host:/opt/gowiki/
rsync -a frontend/dist/ user@host:/opt/gowiki/frontend/dist/
rsync -a backend/data/ user@host:/opt/gowiki/data/

# 3. Create config on server (/opt/gowiki/config.yaml)
#
# data_dir: /opt/gowiki/data
# server:
#   tls_domain: wiki.example.com
#   web_dir: /opt/gowiki/frontend/dist
# site:
#   title: My Wiki
#   base_url: https://wiki.example.com

# 4. Systemd unit (/etc/systemd/system/gowiki.service)
# [Unit]
# Description=Gowiki
# After=network.target postgresql.service
#
# [Service]
# Type=simple
# User=gowiki
# ExecStart=/opt/gowiki/gowiki-server -config /opt/gowiki/config.yaml
# Restart=on-failure
# RestartSec=5
#
# [Install]
# WantedBy=multi-user.target

# 5. Start
systemctl enable --now gowiki
```

### DNS

Point `wiki.example.com` A/AAAA record to the server's public IP before starting. Let's Encrypt needs to reach port 80 to validate domain ownership.
