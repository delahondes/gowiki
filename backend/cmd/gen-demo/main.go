package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	dataDir := "data"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}
	metaDir := filepath.Join(dataDir, "meta")
	contentDir := filepath.Join(dataDir, "content")
	os.MkdirAll(metaDir, 0755)
	os.MkdirAll(contentDir, 0755)

	// --- Users ---
	type User struct {
		Username     string   `json:"username"`
		PasswordHash string   `json:"password_hash"`
		Email        string   `json:"email"`
		DisplayName  string   `json:"display_name"`
		Groups       []string `json:"groups"`
		Disabled     bool     `json:"disabled"`
		CreatedAt    string   `json:"created_at"`
	}
	userDefs := []struct{ name, display, email string; groups []string }{
		{"admin", "Administrator", "admin@example.com", []string{"admin", "editors"}},
		{"alice", "Alice Martin", "alice@example.com", []string{"editors", "quality"}},
		{"bob", "Bob Wilson", "bob@example.com", []string{"editors", "development"}},
		{"tom", "Tom Chen", "tom@example.com", []string{"editors"}},
	}
	var users []User
	for _, u := range userDefs {
		hash, _ := bcrypt.GenerateFromPassword([]byte(u.name), bcrypt.DefaultCost)
		users = append(users, User{
			Username: u.name, PasswordHash: string(hash),
			Email: u.email, DisplayName: u.display,
			Groups: u.groups, CreatedAt: "2026-01-01T00:00:00Z",
		})
	}
	writeJSON(filepath.Join(metaDir, "users.json"), users)
	fmt.Printf("Created %d users\n", len(users))

	// --- Groups ---
	type Group struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	groups := []Group{
		{"admin", "Administrators"},
		{"editors", "Can edit all pages"},
		{"quality", "Quality assurance team"},
		{"development", "Development team"},
	}
	writeJSON(filepath.Join(metaDir, "groups.json"), groups)

	// --- ACL ---
	type ACL struct {
		Pattern     string   `json:"pattern"`
		SubjectType string   `json:"subject_type"`
		Subject     string   `json:"subject"`
		Permissions []string `json:"permissions"`
	}
	acl := []ACL{
		{".*", "group", "admin", []string{"view", "edit", "delete"}},
		{".*", "group", "editors", []string{"view", "edit"}},
		{".*", "special", "@all", []string{"view"}},
	}
	writeJSON(filepath.Join(metaDir, "acl.json"), acl)

	// --- Sessions ---
	writeJSON(filepath.Join(metaDir, "sessions.json"), map[string]any{})

	// --- Content pages ---
	pages := map[string]string{
		"sidebar": `# Acme Wiki

- [Home](/)

# Projects

- [Project Alpha](/projects/alpha)
- [Project Beta](/projects/beta)
- [Project Gamma](/projects/gamma)

# Documentation

- [Getting Started](/docs/getting-started)
- [Architecture](/docs/architecture)
- [API Reference](/docs/api-reference)
- [Deployment Guide](/docs/deployment)

# Team

- [Team Directory](/team)
- [Meeting Notes](/meetings)`,

		"footer": `---
Acme Corp Wiki | Powered by [Gowiki](https://github.com)`,

		"index": `# Welcome to Acme Wiki

This is the central knowledge base for **Acme Corporation**. Here you will find project documentation, team information, and company procedures.

## Quick Links

- [Project Alpha](/projects/alpha) — Our flagship product
- [Getting Started](/docs/getting-started) — New here? Start with this guide
- [Team Directory](/team) — Find your colleagues
- [Meeting Notes](/meetings) — Recent meeting summaries

## Recent Updates

| Date | Page | Author |
| --- | --- | --- |
| 2026-03-15 | [Architecture](/docs/architecture) | Alice Martin |
| 2026-03-12 | [Project Beta](/projects/beta) | Bob Wilson |
| 2026-03-10 | [Deployment Guide](/docs/deployment) | Tom Chen |

## About This Wiki

This wiki uses a **bijective Markdown dialect** — each formatting has exactly one canonical syntax, ensuring lossless round-trips between the visual editor and raw markdown. See the [User Manual](/wiki/manual) for details.`,

		"projects/alpha/index": `{tag project}

# Project Alpha

## 1. Overview

Project Alpha is our flagship product, a *cloud-native* data processing platform designed for **high-throughput genomic analysis**. The platform handles petabyte-scale datasets with sub-second query response times.

## 1. Architecture

The system is built on a _microservices_ architecture with three main components:

- **Ingestion Service** — Handles raw data upload and initial validation
- **Processing Pipeline** — Distributed computation engine using Apache Spark
- **Query Engine** — Real-time query interface with SQL compatibility

For detailed architecture documentation, see [Architecture](/docs/architecture).

## 1. Team

| Role | Person | Contact |
| --- | --- | --- |
| Project Lead | [Alice Martin](/team#alice) | [](mailto:alice@example.com) |
| Lead Developer | [Bob Wilson](/team#bob) | [](mailto:bob@example.com) |
| QA Engineer | [Tom Chen](/team#tom) | [](mailto:tom@example.com) |

## 1. Status

Current version: **2.3.1**
Next release: 2026-04-01

{todo title="Review Alpha release notes" assign="alice" due=2026-04-01}

{todo title="Update deployment documentation" assign="bob" due=2026-03-25}

## 1. Related Documents

- [API Reference](/docs/api-reference)
- [Deployment Guide](/docs/deployment)
- [Project Beta](/projects/beta) — The successor project`,

		"projects/beta/index": `{tag project}

# Project Beta

## 1. Overview

Project Beta is the next-generation evolution of [Project Alpha](/projects/alpha), incorporating machine learning capabilities and a redesigned user interface.

## 1. Goals

1. Integrate ML-based anomaly detection into the processing pipeline
2. Redesign the query interface with a visual query builder
3. Support multi-tenant deployment with namespace isolation
4. Achieve SOC 2 Type II compliance

## 1. Timeline

| Milestone | Target Date | Status |
| --- | --- | --- |
| Design Review | 2026-02-15 | Completed |
| Prototype | 2026-04-01 | In Progress |
| Alpha Release | 2026-06-15 | Planned |
| GA Release | 2026-09-01 | Planned |

## 1. Technical Notes

The ML component uses a ~~TensorFlow~~ PyTorch backend with custom model serving infrastructure. Key considerations:

- Model training pipeline runs on dedicated GPU nodes
- Inference latency target: \<50ms p99
- Model versioning integrated with the data pipeline

^[The switch from TensorFlow to PyTorch was decided in the 2026-01 architecture review based on team expertise and ecosystem support.]`,

		"projects/gamma": `{tag project}

# Project Gamma

## 1. Overview

Project Gamma is an internal tooling initiative to improve developer productivity. It includes:

- **Code Review Dashboard** — Aggregates PRs across repositories
- **CI/CD Metrics** — Build time and failure rate tracking
- **Documentation Linter** — Ensures wiki pages meet quality standards

## 1. Quick Start

To set up the development environment:

` + "```bash\ngit clone https://github.com/acme/gamma.git\ncd gamma\nmake setup\nmake dev\n```" + `

Configuration file example:

` + "```yaml\nserver:\n  port: 8080\n  debug: true\ndatabase:\n  host: localhost\n  name: gamma_dev\n```" + `

## 1. Contact

Project owner: [Tom Chen](/team#tom)`,

		"docs/getting-started": `# Getting Started

Welcome to Acme Corp! This guide will help you get set up with our tools and processes.

## 1. Account Setup

1. Log in to the wiki with your credentials
2. Update your profile (click your username in the top-right corner)
3. Create an API token if you plan to use AI assistants (see [API Tokens](/wiki/manual/admin-tokens))

## 1. Key Resources

| Resource | Description | Link |
| --- | --- | --- |
| This Wiki | Central knowledge base | You're here! |
| Code Repository | Source code | [GitHub](https://github.com) |
| CI/CD | Build pipeline | [Jenkins](https://jenkins.example.com) |
| Chat | Team communication | [Zulip](https://chat.example.com) |

## 1. Your First Contribution

1. Find a page that needs updating (check the [recent changes](/changes))
2. Click **Edit** in the action bar
3. Make your changes in the visual editor
4. Click **Publish** when done

For markdown syntax details, see the [Syntax Reference](/wiki/manual/syntax).

## 1. Need Help?

- Ask in the #wiki-help chat channel
- Check the [User Manual](/wiki/manual)
- Contact [Alice Martin](/team#alice) (wiki admin)`,

		"docs/architecture": `# Architecture

## 1. System Overview

The Acme platform follows a **microservices architecture** deployed on Kubernetes. Each service is independently deployable and communicates via gRPC and message queues.

## 1. Component Diagram

The platform consists of the following layers:

- **API Gateway** — Routes external requests, handles authentication
- **Core Services** — Business logic microservices
  - Ingestion Service
  - Processing Service
  - Query Service
  - Notification Service
- **Data Layer** — PostgreSQL, Redis, S3-compatible object storage
- **Infrastructure** — Kubernetes, Prometheus, Grafana

## 1. Data Flow

1. Client submits data via the API Gateway
2. Ingestion Service validates and stores raw data in S3
3. Processing Service picks up jobs from the message queue
4. Results are written to PostgreSQL
5. Query Service serves results to clients

## 1. Security

All inter-service communication uses mTLS. External access requires:
- OAuth 2.0 bearer tokens for API access
- Session cookies for web UI access

See [Deployment Guide](/docs/deployment) for infrastructure details.`,

		"docs/api-reference": `# API Reference

## 1. Authentication

All API requests require authentication via Bearer token:

` + "```\nAuthorization: Bearer <token>\n```" + `

## 1. Endpoints

### Data Ingestion

` + "```\nPOST /api/v1/ingest\nContent-Type: application/json\n\n{\"dataset\": \"example\", \"source\": \"upload\"}\n```" + `

### Query

` + "```\nGET /api/v1/query?dataset=example&filter=status:active\n```" + `

### Status

` + "```\nGET /api/v1/health\n\nResponse: {\"status\": \"ok\", \"version\": \"2.3.1\"}\n```" + `

## 1. Rate Limits

| Tier | Read | Write |
| --- | --- | --- |
| Standard | 100/min | 20/min |
| Premium | 1000/min | 200/min |

## 1. Error Codes

| Code | Meaning |
| --- | --- |
| 400 | Bad request — check your parameters |
| 401 | Authentication required |
| 403 | Access denied — insufficient permissions |
| 429 | Rate limit exceeded — see Retry-After header |
| 500 | Internal server error |`,

		"docs/deployment": `# Deployment Guide

## 1. Prerequisites

- Kubernetes cluster (1.28+)
- Helm 3.x
- PostgreSQL 15+
- S3-compatible object storage

## 1. Installation

` + "```bash\n# Add the Acme Helm repository\nhelm repo add acme https://charts.acme.example.com\n\n# Install the platform\nhelm install acme-platform acme/platform \\\n  --namespace acme \\\n  --set database.host=postgres.example.com \\\n  --set storage.endpoint=s3.example.com\n```" + `

## 1. Configuration

Key configuration values:

| Parameter | Default | Description |
| --- | --- | --- |
| replicas | 3 | Number of service replicas |
| database.host | localhost | PostgreSQL hostname |
| storage.endpoint | localhost:9000 | S3 endpoint |
| auth.provider | local | Authentication provider |

## 1. Monitoring

The platform exposes Prometheus metrics on port 9090. Import the provided Grafana dashboards:

` + "```bash\nkubectl apply -f dashboards/\n```" + `

## 1. Troubleshooting

Common issues:

- **Pods in CrashLoopBackOff** — Check database connectivity
- **Slow queries** — Review index configuration
- **Storage full** — Configure retention policies`,

		"team/index": `# Team Directory

## 1. Engineering

| Name | Role | Email | Projects |
| --- | --- | --- | --- |
| Alice Martin | Tech Lead & QA | [](mailto:alice@example.com) | [Alpha](/projects/alpha), [Beta](/projects/beta) |
| Bob Wilson | Senior Developer | [](mailto:bob@example.com) | [Alpha](/projects/alpha), [Beta](/projects/beta) |
| Tom Chen | Developer & DevOps | [](mailto:tom@example.com) | [Gamma](/projects/gamma), Infrastructure |

## 1. Contact

For general inquiries, email [](mailto:team@example.com).
For urgent issues, use the #oncall channel in Zulip.`,

		"meetings/index": `# Meeting Notes

## 1. Upcoming

| Date | Topic | Organizer |
| --- | --- | --- |
| 2026-03-20 | Sprint Review | Alice Martin |
| 2026-03-25 | Architecture Review | Bob Wilson |

## 1. Past Meetings

- [2026-03-13 — Weekly Standup](./2026-03-13)
- [2026-03-06 — Sprint Planning](./2026-03-06)`,

		"meetings/2026-03-13": `# Weekly Standup — 2026-03-13

**Attendees:** Alice Martin, Bob Wilson, Tom Chen

## 1. Updates

**Alice:**
- Completed code review for the ML pipeline integration
- Started writing test cases for the query builder
- _Blocked_ on database migration — waiting for DBA approval

**Bob:**
- Deployed v2.3.1 hotfix to production
- Working on the visual query builder prototype
- Will present at next architecture review

**Tom:**
- Finished CI/CD pipeline optimization — build times reduced by 40%
- Setting up monitoring dashboards for [Project Gamma](/projects/gamma)

## 1. Action Items

{todo title="Submit database migration request" assign="alice" due=2026-03-15 priority=high}

{todo title="Prepare architecture review presentation" assign="bob" due=2026-03-24}

{todo title="Share monitoring dashboard access with team" assign="tom" due=2026-03-14}`,

		"meetings/2026-03-06": `# Sprint Planning — 2026-03-06

**Attendees:** Alice Martin, Bob Wilson, Tom Chen

## 1. Sprint Goals

1. Complete ML pipeline integration (Alpha)
2. Visual query builder prototype (Beta)
3. CI/CD metrics dashboard (Gamma)

## 1. Story Points

| Story | Assignee | Points |
| --- | --- | --- |
| ML Pipeline Tests | Alice | 8 |
| Query Builder UI | Bob | 13 |
| CI Metrics API | Tom | 5 |
| Documentation Update | Tom | 3 |

**Total:** 29 points (capacity: 32)`,
	}

	for path, content := range pages {
		fullPath := filepath.Join(contentDir, path+".md")
		// If path contains /, it might be a namespace index
		if filepath.Base(path) == "index" {
			fullPath = filepath.Join(contentDir, path+".md")
		}
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}
	fmt.Printf("Created %d pages\n", len(pages))
}

func writeJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0644)
}
