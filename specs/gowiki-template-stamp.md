# Feature request — creating a document from a template, with a stamped origin

Raised 2026-09-18. The measurements quoted throughout come from one corpus — a
QMS of 51 templates — cited as evidence of the shape of the problem, not as its
boundary.

## 1. Problem

A document produced from a template should say which template produced it, and
in which version. The question it answers is asked wherever forms evolve: *was
this written on the current form, or on one since superseded?* Regulated settings
make it explicit — ISO 13485 §4.2.4 for a quality system — but a handbook, a
report series or a meeting record raises the same question as soon as its form
changes.

Gowiki templates are copied by hand. The page holds a tracking block, a
horizontal rule, and below it the section a user selects and pastes into a new
page. Nothing in that gesture tells the wiki that a copy happened, so the origin
has to be written into the template in advance, as ordinary text:

```markdown
Source template: [SOFT/SOP01/TPL10 : Verification and Validation Plan](/regulatory/qms/soft/sop01/tpl10)
Template version: 1.0
```

**That duplicates the template's version.** It is already held in the tracking
block's `{reviewflow version=…}`, above the rule. Two copies of one fact, kept in
step by hand.

They drift. In the measured corpus, 13 templates carried such a block and six
announced a version the template had left behind — five of them moved from 1.0 to
1.1 on a single day six months earlier, their blocks left behind. Five other
blocks named a different template than the page they sat on, an artefact of
writing the same two lines fifty times. No one made a mistake; the redundancy
suffices.

**The frozen value is the point.** A document carries the version it was *issued
from*. A directive resolving the template's current version at render time would
make a document from 2024 claim today's form, turning a record of what happened
into a moving statement.

So the value must be frozen, but frozen **at the moment of the copy** rather than
written by hand months in advance. That requires the wiki to perform the copy.

A second hand-built mechanism sits next to the first. So the created document
gets its own review flow, templates carry an **escaped** directive below the
rule:

```markdown
\{reviewflow version=1.0 author=alice.laporte reviewer=raynald.delahondes validation=etienne.formstecher\}
```

inert in the template, live once pasted. It depends on the escaping surviving a
copy-paste, and breaks in any editor that resolves it. In the measured corpus, 47
of the 48 templates with a copiable section follow one convention here — version
1.0, and the template's own actors — and one departs from it, which is why the
actors stay overridable in §2.

## 2. Proposed syntax

Four directives. None is about review flow as such — a template with no
`reviewflow` at all is a legitimate case (see §5) — so the family is named after
templating.

| Directive | Lives in | Fate at creation |
|---|---|---|
| `{template}` | Template only | Not copied. Marks where the copiable payload begins, and carries the action. |
| `{template-title}` | Template and document | Prefixes the heading that is the document's title. Resolved to that heading. |
| `{template-stamp}` | Template and document | Replaced by the origin sentence. |
| `{template-reviewflow …}` | Template only | Replaced by `{reviewflow …}` in the created document. |

### `{template}`

Replaces the horizontal rule used today. It renders as that rule, plus a
**Create document** action. Everything strictly below it is the payload.

Being the delimiter, it also declares to the engine that the page is a template.

### `{template-title}`

A prefix directive, applying to the block on the next line — the convention
already used by `{image size=…}`:

```markdown
{template-title}
# Verification and validation plan - ==[Name of the Medical Device]==
```

The pattern stays a real heading, so the template page still renders with one,
and the pattern is written in the dialect rather than trapped in an attribute.

The pattern is **prefill for a human**, not a substitution language. Title
conventions vary by document family and by deployment; the measured corpus alone
holds two irreconcilable shapes, `<ACRONYM>/SOFTxx(:vX) : <title>` for seven
titles and `<Document name> - ==[what identifies it]==` for some forty. A
substitution syntax should be designed against conventions that have settled, so
here the dialog presents the pattern and the user edits it. Optional arguments
stay available for that future.

### `{template-stamp}`

Takes no argument. In the created document it becomes:

```
Created from template [SOFT/SOP01/TPL10 : Verification and Validation Plan](/regulatory/qms/soft/sop01/tpl10), version 1.0
```

The format is fixed by the wiki rather than by each template: left to authors it
becomes one variant per template, which is the state this request exists to
leave.

Version resolution: the template's `VERSIONTAG` when it has a review flow,
falling back to `VERSION` when it has none. **The two should not be worded
alike.** `VERSION` is the wiki's revision counter, not a document version; a
document announcing `version 77` beside one announcing `version 1.1` invites a
reader to treat the two numbers as the same kind of thing. Suggested rendering:
`version 1.1` in the first case, `revision 77` in the second.

In the template page itself the directive renders as a note — `Created from
template: stamped on creation` or similar. A template is also a document that
people read and review; it should not display a blank.

### `{template-reviewflow …}`

Same arguments as `{reviewflow}`, all optional:

```markdown
{template-reviewflow}
{template-reviewflow author=alice.laporte reviewer=raynald.delahondes validation=etienne.formstecher}
```

Defaults, matching what 47 of 48 templates already do by hand:

| Argument | Default |
|---|---|
| `version` | `1.0` |
| `author`, `reviewer`, `validation` | those of the template's own `{reviewflow}` |

It replaces the escaped-directive workaround entirely.

### Composition with existing page templates

Gowiki already has a notion of template: pages bound to a database row through
`{{field}}` placeholders, such as `capa_template` or `provider_template`. The
directives above do not compete with it, and should be usable inside one.

A row-bound page is a document too, and nothing in it says which version of its
page template produced it — the same gap, reached by another path. A
`{template-stamp}` placed in a `{{field}}` template should therefore resolve when
the row-bound page is created, by the same rule and to the same frozen result.
The creation path differs — no dialog, the row supplies the identity — but the
stamp does not.

`{template-title}` and `{template-reviewflow}` have no evident use there, the
title coming from the row and such pages carrying no review flow of their own.
They should be ignored rather than rejected, so that one template can serve both
paths.

## 3. The Create document action

Triggered from `{template}`. It asks for a **destination path**, not just a name:
a document rarely belongs beside the template that produced it — it belongs in
the file, project or namespace it documents. It also presents the title,
prefilled from `{template-title}`, for the user to complete.

It then creates the page from everything below `{template}`, resolving the three
payload directives:

1. `{template-title}` → the heading as the user completed it.
2. `{template-stamp}` → the origin sentence, with the template's version frozen.
3. `{template-reviewflow …}` → `{reviewflow …}` with defaults applied.

Everything else is copied verbatim, including the `{tag rec}` that opens the
payload today.

### Through the MCP

**The action belongs in the MCP as much as in the interface.** Pages are created
programmatically wherever an agent or a script maintains part of a wiki, and on
that path a template reachable only from a browser is a template written out by
hand — the state this request exists to leave. The MCP is where the guarantee
holds or fails.

A tool, say `create_page_from_template`:

| Parameter | Required | Description |
|---|---|---|
| `template_path` | yes | The template page |
| `path` | yes | Destination, canonical |
| `title` | yes | The document's first heading, replacing the pattern |
| `reviewflow` | no | Overrides for the created `{reviewflow}` |
| `summary` | yes | As for every write operation |

It resolves the three payload directives exactly as the button does, and applies
the same guards: refusal on an unvalidated template, refusal if `path` already
exists. The result should name the version it stamped, so a caller can record
what was issued without re-reading the page.

No companion tool is needed to discover the title pattern: it is a heading on the
template page, which `read_pages_batch` already returns.

## 4. Worked example

`SOFT/SOP01/TPL10`, version 1.0, below its `{template}` rule:

```markdown
{tag rec}

{template-title}
# Verification and validation plan - ==[Name of the Medical Device]==

## 1. Follow-up and approval

### 1. Authors and reviewers

{template-reviewflow}

### 1. Change history

{template-stamp}

| Version | Date | Description | Author |
| --- | --- | --- | --- |
| 1.0 |  | Initial version |  |
```

Creating `/regulatory/ioscope/2/vvp` from it, titled
`VVP/SOFT02 : Verification and Validation Plan`, yields:

```markdown
{tag rec}

# VVP/SOFT02 : Verification and Validation Plan

## 1. Follow-up and approval

### 1. Authors and reviewers

{reviewflow version=1.0 author=raynald.delahondes reviewer=michel.laborde validation=etienne.formstecher}

### 1. Change history

Created from template [SOFT/SOP01/TPL10 : Verification and Validation Plan](/regulatory/qms/soft/sop01/tpl10), version 1.0

| Version | Date | Description | Author |
| --- | --- | --- | --- |
| 1.0 |  | Initial version |  |
```

Nothing in the result needs maintaining, and nothing in the template holds a
version number written by hand.

## 5. Behaviours we depend on

**An unresolved stamp must be loud.** Copy-paste stays possible, and then
`{template-stamp}` lands in a document unresolved. On a page with no `{template}`,
it must render a visible error rather than nothing or its own source. This is the
failure that would otherwise be found at audit rather than at writing time.

**Creation from an unvalidated template should refuse, or warn.** Stamping a
version whose review flow is still open amounts to issuing a document on a
form not yet approved. If refusing is too strong, the dialog should say so plainly and
record that it was overridden.

**Templates without a review flow are in scope.** Some templates carry no
`{reviewflow}` — the `VERSION` fallback above exists for them. They must be able
to use `{template}`, `{template-title}` and `{template-stamp}` normally;
`{template-reviewflow}` is simply omitted, and no review flow is created.

**A stamp is written once, at creation.** It states which template produced this
document and in which version, and both remain true whatever the template does
afterwards. Should the template later move, the link goes stale and says so,
which is the honest outcome: the stamp answers what was used, not what exists
now.

## 6. Worth considering

**Pin the link to the version.** If the stamp linked the template *at that
revision* rather than at its current state, a reader would see the form as it
stood when the document was issued, without reconstructing it from history. The
history is already retained, so the cost is in the URL form alone.

**A reverse view.** Once the link is recorded, the wiki can answer a question
nobody can ask today: *which documents were issued from a template version since
superseded?* That question arrives whenever a form changes, and it is arguably
worth more than the traceability line itself.

## 7. Out of scope

- Substitution inside the title pattern. Left for later: two placeholder
  syntaxes already coexist in the measured corpus (`==[…]==` and an optional
  `(:vX)` suffix), and a flexible syntax deserves to be designed against settled
  conventions.
- Documents already created. The feature acts at creation.
- Templates that generate several pages at once.

## 8. Migration

In the corpus that prompted this request, 48 templates carry a hand-written
traceability block and an escaped `\{reviewflow …\}`. Converting them is
mechanical: the block becomes
`{template-stamp}`, the escaped directive becomes `{template-reviewflow}`, the
rule becomes `{template}`, and the payload heading gains `{template-title}`. It
can be done in one pass and does not gate the implementation.
