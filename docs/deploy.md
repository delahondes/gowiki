# Deploying Gowiki

This guide covers deploying Gowiki on a Linux VPS with automatic HTTPS via Let's Encrypt. The target is a minimal setup: one binary, one config file, one Postgres database.

## Prerequisites

- A Linux server (Ubuntu 22.04+ or Debian 12+ recommended), 2 GB RAM, 1 vCPU
- A domain name pointed to the server's public IP (A record)
- Ports 80 and 443 open in the firewall
- PostgreSQL 14+ installed (needed for todo, database plugin, and structured data)

## 1. Build

On your development machine:

```bash
# Build the frontend bundle
make build-frontend

# Cross-compile the Go binary for Linux
cd backend
GOOS=linux GOARCH=amd64 go build -o gowiki-server ./cmd/server
cd ..
```

This produces two artifacts:
- `backend/gowiki-server` — the server binary
- `frontend/dist/` — the built frontend assets

## 2. Prepare the server

```bash
# Create a dedicated user
sudo useradd -r -m -d /opt/gowiki -s /bin/bash gowiki

# Create directory structure
sudo mkdir -p /opt/gowiki/{frontend/dist,data}
sudo chown -R gowiki:gowiki /opt/gowiki
```

## 3. Copy files

From your dev machine:

```bash
SERVER=user@your-server

# Binary
scp backend/gowiki-server $SERVER:/opt/gowiki/

# Frontend assets
rsync -a --delete frontend/dist/ $SERVER:/opt/gowiki/frontend/dist/

# Data (first deploy only — contains pages, media, auth, metadata)
rsync -a backend/data/ $SERVER:/opt/gowiki/data/
```

On subsequent deploys, only copy the binary and frontend assets. Do not overwrite `data/` — it contains your wiki content and configuration.

## 4. Set up PostgreSQL

```bash
sudo -u postgres createuser gowiki
sudo -u postgres createdb -O gowiki gowiki
```

If you need a password (remote connections or `md5` auth):

```bash
sudo -u postgres psql -c "ALTER USER gowiki PASSWORD 'your-secure-password';"
```

## 5. Create the config file

Create `/opt/gowiki/config.yaml`:

```yaml
data_dir: /opt/gowiki/data

server:
  tls_domain: wiki.example.com
  web_dir: /opt/gowiki/frontend/dist

site:
  title: My Wiki
  base_url: https://wiki.example.com

auth:
  session_ttl: 72h

database:
  enabled: true
  dsn: postgres://gowiki:your-secure-password@localhost/gowiki?sslmode=disable

drafts:
  auto_save_interval: 2m
  stale_lock_timeout: 24h
```

Adjust `tls_domain`, `base_url`, and `dsn` to match your setup. If PostgreSQL uses local peer auth (no password), the DSN is simply `postgres:///gowiki`.

**Important:** Always set `site.base_url` in production. When configured, the server rejects any HTTP request whose `Host` header does not match the expected hostname (returning a 404). This prevents the server from responding to requests via raw IP address or unexpected domain names pointing to the same server.

## 6. Create the systemd service

Create `/etc/systemd/system/gowiki.service`:

```ini
[Unit]
Description=Gowiki
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=gowiki
Group=gowiki
ExecStart=/opt/gowiki/gowiki-server -config /opt/gowiki/config.yaml
Restart=on-failure
RestartSec=5

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/gowiki/data
PrivateTmp=true

# Allow binding to ports 80 and 443
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

The `CAP_NET_BIND_SERVICE` capability lets the `gowiki` user bind to privileged ports (80, 443) without running as root.

## 7. Start the service

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gowiki
```

Check that it started correctly:

```bash
sudo systemctl status gowiki
sudo journalctl -u gowiki -f
```

You should see logs like:

```
config: /opt/gowiki/config.yaml
serving frontend assets from /opt/gowiki/frontend/dist
data root: /opt/gowiki/data
http: listening on :80 (ACME + redirect)
https: listening on :443 (domain: wiki.example.com)
```

The first HTTPS request triggers certificate provisioning from Let's Encrypt. This takes a few seconds. Subsequent requests use the cached certificate.

## 8. Verify

```bash
curl -I https://wiki.example.com
```

You should get a `200 OK` with a valid TLS certificate.

## Updating

To deploy a new version:

```bash
# Build locally
make build-frontend
cd backend && GOOS=linux GOARCH=amd64 go build -o gowiki-server ./cmd/server && cd ..

# Copy to server
scp backend/gowiki-server $SERVER:/opt/gowiki/gowiki-server.new
rsync -a --delete frontend/dist/ $SERVER:/opt/gowiki/frontend/dist/

# On the server: swap binary and restart
ssh $SERVER '
  sudo systemctl stop gowiki
  mv /opt/gowiki/gowiki-server.new /opt/gowiki/gowiki-server
  sudo systemctl start gowiki
'
```

The graceful shutdown waits up to 10 seconds for in-flight requests to complete before stopping.

## Backups

The critical data to back up:

| Path | Contents |
|------|----------|
| `/opt/gowiki/data/content/` | All wiki pages and media files |
| `/opt/gowiki/data/meta/` | Users, groups, ACL, sessions, page metadata |
| `/opt/gowiki/data/attic/` | Page version history |
| `/opt/gowiki/config.yaml` | Site configuration |
| PostgreSQL `gowiki` database | Todo tasks, structured data tables |

Example daily backup with cron:

```bash
# /etc/cron.daily/gowiki-backup
#!/bin/bash
BACKUP_DIR=/var/backups/gowiki/$(date +%Y-%m-%d)
mkdir -p "$BACKUP_DIR"
rsync -a /opt/gowiki/data/ "$BACKUP_DIR/data/"
cp /opt/gowiki/config.yaml "$BACKUP_DIR/"
pg_dump -U gowiki gowiki | gzip > "$BACKUP_DIR/gowiki.sql.gz"
# Keep 30 days
find /var/backups/gowiki -maxdepth 1 -mtime +30 -exec rm -rf {} +
```

## Running without TLS

If you're behind a load balancer or reverse proxy that handles TLS, remove `tls_domain` and set `addr`:

```yaml
server:
  addr: ":8080"
  web_dir: /opt/gowiki/frontend/dist
```

The server listens on plain HTTP. Your reverse proxy should forward to `:8080` and set `X-Forwarded-Proto: https`.

In this mode, remove `AmbientCapabilities=CAP_NET_BIND_SERVICE` from the systemd unit since port 8080 doesn't require elevated privileges.

## Troubleshooting

**Certificate provisioning fails**
- Verify DNS: `dig wiki.example.com` must return the server's public IP.
- Verify port 80 is reachable: `curl http://wiki.example.com` from another machine.
- Check firewall: `sudo ufw status` or `sudo iptables -L -n`.
- Check logs: `journalctl -u gowiki -e` for ACME errors.

**"permission denied" binding to port 80/443**
- Ensure `AmbientCapabilities=CAP_NET_BIND_SERVICE` is in the systemd unit.
- Run `sudo systemctl daemon-reload` after editing the unit file.

**Database connection refused**
- Check PostgreSQL is running: `sudo systemctl status postgresql`.
- Check the DSN in config.yaml matches your PostgreSQL auth method (`peer` vs `md5`).
- For local peer auth, the system user (`gowiki`) must match the PostgreSQL role name.

**OAuth callback URL mismatch**
- Update `site.base_url` in config.yaml to match the public URL exactly.
- Update the callback URL in your OAuth provider (Azure AD, etc.) to `https://wiki.example.com/api/auth/oauth/callback`.
