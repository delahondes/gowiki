# Lifecycle Rules

Lifecycle rules let you write a **single directive on one admin page** that fires a todo across every page in scope when a condition is met. The rule stays DRY — you write it once — but its consequences fan out per target page so each stale page becomes its own actionable, individually-completable todo.

Today the only supported condition is `stale`: no attestation for a configured duration. The framework is condition-pluggable, so new triggers can be added without changing the directive shape.

## 1. What a rule looks like

Placed anywhere you like — typically on a `/admin/policies` or `/qms/lifecycle-rules` page:

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

On the next scan pass, one todo appears for every page under `/qms/soft/…` that carries at least one of `sop`, `rec`, or `tpl`, is NOT tagged `archived`, and has had no content edit or reviewflow validation in the last 30 months. When someone edits or re-validates one of those pages, the todo it generated is auto-cancelled on the next scan.

## 1. Selectors

A page is in scope for a rule iff **every present selector matches**:

| Selector | Meaning | Example |
|---|---|---|
| `scope=` | Page path must match this regex | `scope="^/qms/.*"` |
| `tags=` | Page must carry AT LEAST ONE of these tags (OR) | `tags="sop,rec,tpl"` |
| `exclude_tags=` | Page must carry NONE of these tags | `exclude_tags="archived,draft"` |

- Missing selectors are "don't care" for that dimension.
- **At least one of `scope=` or `tags=` must be present.** A rule with only `exclude_tags=` would target the whole wiki minus a few exceptions, which is almost never what you meant.
- `scope` uses the same regex convention as `scope_regexp` on database tables — anchored with `^` and `$` when needed.

## 1. The `stale` condition

`when=stale:<duration>` fires when a page has had no attestation of any kind for at least `<duration>`.

Duration units:

| Suffix | Meaning |
|---|---|
| `d` | days (`stale:90d`) |
| `m` | **months** = 30 days (`stale:30m` = 900 days) |
| `y` | years = 365 days (`stale:2y`) |

Note: `m` means MONTHS here, not minutes — a rule meant "30 months stale" that fired at "30 minutes" would flood the todo store instantly, so we deliberately don't accept `s`, `h`, or Go's `time.ParseDuration` shorthand.

### What counts as "attestation"

An attestation is any timestamped event that says "this page is still what someone means it to be." The `stale` condition takes the MAXIMUM across every source and fires when `now − max > duration`.

Today two sources feed the freshness signal:

1. **Content edit** — `page.meta.updated_at`, bumped every time the page is saved.
2. **Reviewflow validation** — the most recent `VersionRecord.Timestamp` in the page's reviewflow state. Note that reviewflow validation writes to the state file only; it does NOT bump `page.updated_at`. That's why the freshness check consults both.

So a page validated last week by the reviewer chain will NOT show up as stale even if the content itself hasn't been edited in three years — the validation counts.

Future attestation sources (a "seen this" acknowledgement button, a resolved-comment thread, an `owner_last_checked` database field) can be added without changing rule syntax.

## 1. Todo template

The rule's `title=` / `assign=` / `priority=` / `action=` / `todo_tags=` become the fields of every todo the rule creates. String fields support template variables resolved against the target page at scan time:

| Variable | Meaning |
|---|---|
| `{{path}}` | The target page's canonical path |
| `{{last_attested}}` | RFC-3339 timestamp of the most recent attestation, or `never` if none |
| `{{stale_days}}` | Whole days since the last attestation, or `∞` for never-attested |

Unknown names stay literal (a typo like `{{ptah}}` reaches the generated todo unchanged, which is visible enough to notice — silently substituting empty string would be worse).

## 1. When rules fire and unfire

- **Rule save.** Editing a page that carries `{lifecycle}` directives (add / change / remove) updates the index immediately. Removing the rule directive on the next save cancels every todo the rule spawned within one scan pass.
- **Rule deleted (page deleted).** Same: the rule disappears from the index, the todos are reconciled away on the next scan.
- **Target page becomes fresh.** A stale target that gets edited or re-validated stops matching the condition on the next scan; the todo is cancelled.
- **Target page moves into scope.** A page tagged `sop` that was previously outside `/qms/soft/…` and gets moved there matches the rule on the next scan and gets its todo.
- **The rule's own source page.** A rule never fires on the page where it's declared — the author would only be nagging themselves.

## 1. Scan cadence

The scanner runs every 6 hours, plus one immediate pass at server startup. Todos appear or disappear within one cycle of the underlying page change. For a change you want reflected sooner, restart the service — the initial pass on boot picks up everything.

## 1. Idempotency

Every todo the scanner creates carries a deterministic node key derived from `sha1("lifecycle:" + rule_id + ":" + target_page)`. So re-running the scan against the same state creates no duplicates, and manual completions / cancellations of a lifecycle todo persist across scans as long as the rule and the stale-ness condition still hold. If the rule stops firing on that page (edit, re-validation, out-of-scope move, rule deletion), the todo is cancelled on the next pass regardless of its current status.
