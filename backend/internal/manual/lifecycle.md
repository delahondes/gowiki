# Lifecycle Rules

Lifecycle rules let you write a **single directive on one admin document** that fires a todo across every document in scope when a condition is met. The rule stays DRY — you write it once — but its consequences fan out per target document so each stale document becomes its own actionable, individually-completable todo.

Today the only supported condition is `stale`: no attestation for a configured duration. The framework is condition-pluggable, so new triggers can be added without changing the directive shape.

## 1. What a rule looks like

Placed anywhere you like — typically on a `/admin/policies` or `/qms/lifecycle-rules` document:

```markdown
{lifecycle
  scope="^/qms/soft/.*"
  tags="sop,rec,tpl"
  exclude_tags="archived"
  when=stale:30m
  title="Please review {{path}} (last attested {{stale_days}} days ago)"
  assign=qms-lead
  priority=normal
}
```

On the next scan pass, one todo appears for every document under `/qms/soft/…` that carries at least one of `sop`, `rec`, or `tpl`, is NOT tagged `archived`, and has had no content edit or reviewflow validation in the last 30 months. When someone edits or re-validates one of those documents, the todo it generated is auto-cancelled on the next scan.

## 1. Selectors

A document is in scope for a rule iff **every present selector matches**:

| Selector | Meaning | Example |
|---|---|---|
| `scope=` | Document path must match this regex | `scope="^/qms/.*"` |
| `tags=` | Document must carry AT LEAST ONE of these tags (OR) | `tags="sop,rec,tpl"` |
| `exclude_tags=` | Document must carry NONE of these tags | `exclude_tags="archived,draft"` |

- Missing selectors are "don't care" for that dimension.
- **At least one of `scope=` or `tags=` must be present.** A rule with only `exclude_tags=` would target the whole wiki minus a few exceptions, which is almost never what you meant.
- `scope` uses the same regex convention as `scope_regexp` on database tables — anchored with `^` and `$` when needed.

## 1. Conditions

Every rule needs a `when=` condition. Two kinds are supported today:

| Kind | Payload | Produces todos? | Purpose |
|---|---|---|---|
| `stale:<duration>` | `stale:30m` (30 months) | yes | Fires when a document has had no attestation for at least the duration. |
| `reviewflow_overdue` | none | no (panel-only alert) | Fires when a document's reviewflow has one or more roles marked overdue by reviewflow's own deadline configuration. |

Alert-only conditions (like `reviewflow_overdue`) do not need `title=` — no todo is created, so there is nothing to title. `assign=` is optional but still meaningful: it names the person or group the alert is addressed to, so future notification channels (email, dashboard) know who to reach. The lifecycle panel always surfaces the count regardless of `assign=`, so a QARA admin sees the state at a glance next to the other rules.

## 1. The `stale` condition

`when=stale:<duration>` fires when a document has had no attestation of any kind for at least `<duration>`.

Duration units:

| Suffix | Meaning |
|---|---|
| `d` | days (`stale:90d`) |
| `m` | **months** = 30 days (`stale:30m` = 900 days) |
| `y` | years = 365 days (`stale:2y`) |

Note: `m` means MONTHS here, not minutes — a rule meant "30 months stale" that fired at "30 minutes" would flood the todo store instantly, so we deliberately don't accept `s`, `h`, or Go's `time.ParseDuration` shorthand.

### What counts as "attestation"

An attestation is any timestamped event that says "this document is still what someone means it to be." The `stale` condition takes the MAXIMUM across every source and fires when `now − max > duration`.

Today two sources feed the freshness signal:

1. **Content edit** — `page.meta.updated_at`, bumped every time the document is saved.
2. **Reviewflow validation** — the most recent `VersionRecord.Timestamp` in the document's reviewflow state. Note that reviewflow validation writes to the state file only; it does NOT bump `page.updated_at`. That's why the freshness check consults both.

So a document validated last week by the reviewer chain will NOT show up as stale even if the content itself hasn't been edited in three years — the validation counts.

Future attestation sources (a "seen this" acknowledgement button, a resolved-comment thread, an `owner_last_checked` database field) can be added without changing rule syntax.

## 1. Todo template

The rule's `title=` / `assign=` / `priority=` / `action=` / `todo_tags=` become the fields of every todo the rule creates. String fields support template variables resolved against the target document at scan time:

| Variable | Meaning |
|---|---|
| `{{path}}` | The target document's canonical path |
| `{{last_attested}}` | RFC-3339 timestamp of the most recent attestation, or `never` if none |
| `{{stale_days}}` | Whole days since the last attestation, or `∞` for never-attested |

Unknown names stay literal (a typo like `{{ptah}}` reaches the generated todo unchanged, which is visible enough to notice — silently substituting empty string would be worse).

## 1. When rules fire and unfire

- **Rule save.** Editing a document that carries `{lifecycle}` directives (add / change / remove) updates the index immediately. Removing the rule directive on the next save cancels every todo the rule spawned within one scan pass.
- **Rule deleted (document deleted).** Same: the rule disappears from the index, the todos are reconciled away on the next scan.
- **Target document becomes fresh.** A stale target that gets edited or re-validated stops matching the condition on the next scan; the todo is cancelled.
- **Target document moves into scope.** A document tagged `sop` that was previously outside `/qms/soft/…` and gets moved there matches the rule on the next scan and gets its todo.
- **The rule's own source document.** A rule never fires on the document where it's declared — the author would only be nagging themselves.

## 1. Scan cadence

The scanner runs every 6 hours, plus one immediate pass at server startup. Todos appear or disappear within one cycle of the underlying document change. For a change you want reflected sooner, restart the service — the initial pass on boot picks up everything.

## 1. Idempotency

Every todo the scanner creates carries a deterministic node key derived from `sha1("lifecycle:" + rule_id + ":" + target_document)`. So re-running the scan against the same state creates no duplicates, and manual completions / cancellations of a lifecycle todo persist across scans as long as the rule and the stale-ness condition still hold. If the rule stops firing on that document (edit, re-validation, out-of-scope move, rule deletion), the todo is cancelled on the next pass regardless of its current status.

## 1. What the rendering box says

The card shown in the wiki aggregates ALL `{lifecycle}` rules on the current document into ONE panel. When every rule is in order the reader sees a short italic line — *"All documents in scope are within all rules."* — with a small `details` marker per rule for scope/tags/condition/assignee on hover.

When any rule fires, the panel turns red with a plain count (*"12 documents need review."*) and a per-rule bullet list so it's clear which rule needs attention. Each firing bullet carries a `▸ show N documents` disclosure that expands to a scrollable, linked list of the flagged documents — click any path to go straight to it. Above 500 firing documents the count stays honest and the first 500 are listed.

In **visual edit mode** each `{lifecycle}` directive on the page also shows as a small clickable chip (`Lifecycle · <title>`) so the author can click into that specific rule's property panel. In **view mode** the chips are hidden — readers only see the aggregate panel, which already covers every rule on the page.
