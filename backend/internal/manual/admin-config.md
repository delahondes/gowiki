# Configuration

Access: Admin > Configuration

## 1. Site settings

- **Site Title** — displayed in the banner
- **Base URL** — the public URL of the wiki (e.g. `https://wiki.example.com`)
- **Sidebar Page** — page used as the sidebar (default: `sidebar`)
- **Footer Page** — page used as the footer (default: `footer`)
- **TOC Max Level** — maximum heading level shown in the table of contents (0 = disabled)
- **User Display** — how usernames are shown: login, full name, or email
- **Code Theme** — syntax highlighting theme for code blocks

## 1. Authentication

- **Session TTL** — how long sessions last (e.g. `24h`, `168h`)

## 1. OAuth / Microsoft 365

- **Provider** — `azure` or disabled
- **Tenant ID**, **Client ID**, **Client Secret** — Azure AD application credentials
- **Auto-create users** — create user accounts on first OAuth login
- **Default groups** — groups assigned to auto-created users

## 1. Drafts

- **Auto Save Interval** — how often drafts are saved (e.g. `2m`)
- **Stale Lock Timeout** — when locks expire (e.g. `24h`)

## 1. AI Content API

- **Enable** — master switch for token-based API access
- **Read/Write rate limits** — requests per minute per token
- **Max tokens per user** — maximum API tokens a user can create
- **Require summary** — enforce summaries on token-authenticated writes

## 1. Reviewflow

- **Enable** — activate the document validation workflow
- **Deadlines** — per-role timeouts (e.g. `reviewer=72h`)

## 1. Todo

- **Disable** — deactivate the todo plugin (requires restart)
- **Reminder hours** — hours before due date to send reminders

## 1. Email / SMTP

Configure outbound email for todo notifications:
- **From address**, **SMTP host/port**, **username/password**

## 1. Webhooks

Configure outbound webhooks for notifications (Slack, Zulip, etc.):
- **URL**, **HMAC secret**, **enabled/disabled** per webhook
