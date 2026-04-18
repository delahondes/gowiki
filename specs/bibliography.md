# Bibliography — Specification

## 1. Overview

The bibliography plugin lets authors cite scientific publications inline and have a formatted reference list rendered elsewhere on the page. It follows the footnote model already in Gowiki: an inline marker with a hover popup, plus a cumulative list at a predictable location.

Citations are declared by identifier (PubMed ID or DOI) — the plugin fetches the publication's metadata once, caches it, and renders the citation consistently everywhere it appears.

### Design goals

- **One directive, one publication** — `{publication pmid=...}` or `{publication doi=...}` names a single source.
- **No free-form bibliography entries** — every cited publication has a resolvable external identifier. This keeps the data clean and enables automatic link-out.
- **Cache-first rendering** — pages render without hitting the network. A metadata cache under `data/meta/bibliography/` holds the resolved title, authors, year, journal, URL.
- **Same-page aggregation** — the plugin auto-builds a References section at page bottom, or wherever a `{references}` directive is placed.
- **Consistent with footnotes** — hover shows full details; click opens the source's canonical external page (PubMed or DOI resolver).

### Non-goals (v1)

- Cross-page bibliography aggregation (each page renders its own list)
- Citation styles beyond author-year inline + alphabetical list at the end
- Numbered citations (`[1]`, `[2]`) — author-year only for v1
- Custom inline text override (`text="..."` attribute) — defer
- Multiple identifiers per directive — use adjacent directives
- Identifier types beyond PMID and DOI — defer
- Per-user or per-wiki citation style configuration — defer

---

## 2. Directive syntax

### Inline citation

```
{publication pmid=38480887}
{publication doi="10.1038/s41586-024-07067-4"}
```

Exactly one of `pmid` or `doi` must be present. If both or neither are given, the directive renders as an error badge (`[citation: missing identifier]`).

Attribute reference:

| Attribute | Type | Notes |
|---|---|---|
| `pmid` | integer string | PubMed ID |
| `doi` | string | DOI (may contain slashes, quote if needed) |

The directive is self-contained (no body), follows the standard Gowiki directive grammar, and can appear anywhere inline text is allowed: paragraphs, table cells, list items.

### References list

By default, the plugin appends a **References** section at the bottom of the page containing every publication cited by the page's `{publication ...}` directives, sorted alphabetically by first author. The heading text is "References" and is rendered as an `##` heading.

To choose a different location, place a `{references}` directive where the list should appear. When present, the automatic append is suppressed.

```
## Further reading

{references}

## Appendix
```

Attribute reference:

| Attribute | Type | Notes |
|---|---|---|
| `title` | string | Heading for the section (default "References") |
| `show_heading` | boolean | If `false`, suppresses the heading and renders only the list (default `true`) |

---

## 3. Rendering

### Inline form

- 1 author: `[Derosa, 2024]`
- 2 authors: `[Derosa & Smith, 2024]`
- 3+ authors: `[Derosa et al., 2024]`

The marker is a hyperlink. The `href` is the canonical external URL:
- For a PMID: `https://pubmed.ncbi.nlm.nih.gov/<pmid>/`
- For a DOI: `https://doi.org/<doi>`

The link opens in a new tab (`target="_blank"`, `rel="noopener noreferrer"`).

### Hover popup

On hover the marker shows a popup (same visual style as the footnote popup) containing:

```
<Title of the publication>
<First author> (<Year>)
<Journal>
```

### Reference list item

Each list entry is rendered as:

```
Derosa L, Iebba V, Silva CAC, et al. Intestinal Akkermansia muciniphila predicts
clinical response to PD-1 blockade in patients with advanced non-small-cell
lung cancer. Nat Med. 2024;30(2):324-333. PMID: 38480887.
```

Author list policy: up to 6 authors, then "et al.". Format fields: `Authors. Title. Journal. Year;Volume(Issue):Pages. <Identifier>.` Missing fields are skipped cleanly (no empty punctuation).

The same block is also the hover popup's body (keeps the hover/list in sync). The item is clickable and opens the same canonical URL as the inline marker.

### Error states

- Identifier not yet cached and network fetch pending → `[loading]` placeholder
- Identifier not resolvable after retries → `[pmid:38480887 ⚠]` with a tooltip explaining the error; the entry appears in the references list with "Unresolved reference: pmid:38480887"
- Identifier malformed → `[citation: invalid pmid]` inline; no entry added to references

---

## 4. Metadata model

### Cache layout

```
data/meta/bibliography/
  pmid/
    38480887.json
  doi/
    4a3c19f2.json        ← SHA-1 of the DOI (DOIs can contain any printable char)
```

A sidecar `doi/<hash>.json` file stores the raw DOI alongside the metadata, so walking the directory is enough to enumerate all cached entries.

### Schema (both file types)

```jsonc
{
  "identifier_type": "pmid" | "doi",
  "identifier": "38480887",
  "title": "Intestinal Akkermansia muciniphila predicts clinical response to PD-1 blockade ...",
  "authors": [
    { "family": "Derosa", "given": "Lisa" },
    { "family": "Iebba", "given": "Valerio" },
    { "family": "Silva", "given": "Carolina A C" }
    // ... up to the full list as returned by the source
  ],
  "year": 2024,
  "journal": "Nat Med",
  "volume": "30",
  "issue": "2",
  "pages": "324-333",
  "url": "https://pubmed.ncbi.nlm.nih.gov/38480887/",
  "fetched_at": "2026-04-18T10:00:00Z",
  "source": "pubmed_esummary"
}
```

All fields except `identifier_type`, `identifier`, `url`, `fetched_at`, `source` are optional — some sources don't provide every field.

### Fetch sources

- **PMID** → `https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?db=pubmed&id=<PMID>&retmode=json`
- **DOI** → `https://api.crossref.org/works/<DOI>` (accepts the literal DOI in the URL path)

Both return rich JSON. The fetcher extracts the fields above and discards the rest.

### Refresh policy

- Cache entries are treated as permanent. Scientific metadata doesn't change.
- Manual refresh: an admin action (future) can re-fetch a single entry.
- Failed fetches are not cached — the directive stays in `loading` / `unresolved` state until a successful fetch.

### User-Agent

All outbound requests send `User-Agent: Gowiki-Bibliography/1.0 (+<server_url>; mailto:<admin_email>)` per NIH guidelines and Crossref etiquette.

---

## 5. Backend API

### `GET /api/plugin/bibliography/v1/resolve`

Query params: `pmid=<id>` or `doi=<id>`.

Returns the cached entry, or triggers a fetch if none exists. Blocks up to 5 seconds on the first fetch per identifier; subsequent requests are instant.

**Response (200):** the schema from §4 above.

**Response (404):** identifier does not resolve (PubMed returned nothing, Crossref returned 404). Body: `{ "error": "not_found", "identifier": "pmid:38480887" }`.

**Response (503):** external source unreachable. Body: `{ "error": "source_unreachable", "identifier": "pmid:38480887" }`.

### `GET /api/plugin/bibliography/v1/list`

Lists all cached entries (admin-only, for management UI).

**Response (200):**
```json
{
  "entries": [
    { "identifier_type": "pmid", "identifier": "38480887", "title": "...", "year": 2024 },
    { "identifier_type": "doi", "identifier": "10.1038/s41586-024-07067-4", "title": "...", "year": 2024 }
  ]
}
```

### Rate limiting

Per-process token bucket:
- PubMed: 3 req/s (raise to 10/s if `bibliography.pubmed_api_key` is set in config)
- Crossref: 50 req/s (generous default; Crossref is lenient with polite traffic)

Concurrent fetches of the same identifier are deduplicated (singleflight).

---

## 6. Frontend integration

### Plugin module

`frontend/plugins/bibliography.ts` registers:
- Two directive types: `publication` and `references`
- Two ProseMirror nodes: `publication` (inline) and `references` (block)
- A shared cache hydrated from `GET /api/plugin/bibliography/v1/resolve`

The page-level aggregation is a post-render step: after the document is parsed, the plugin walks every `publication` node on the page, deduplicates by identifier, and feeds the sorted list into the `references` node (or appends an auto-generated one at page bottom if no explicit `{references}` directive is present).

### Rendering details

- Inline node: renders as `<a class="gowiki-cite" href="...">[Author, Year]</a>`
- Hover popup: reuses the footnote popup infrastructure (same positioning logic, same CSS class family)
- References list: rendered as a `<ol class="gowiki-references">` with `<li>` items
- Each inline marker carries a unique `data-cite-id` that matches the list item's `id`, so clicking a reference list entry highlights the corresponding inline markers (and vice versa)

### Interaction with the editor

- Toolbar button "Insert publication" opens a dialog: tabs for PMID vs DOI, input, "Resolve" button that previews the resolved citation before inserting.
- Pasting a PubMed URL or DOI URL into the editor auto-converts to a `{publication}` directive (similar to how the link plugin auto-detects URLs).

### Copy/paste semantics

The node copies as its directive form (`{publication pmid=123}`) so pasting into another page preserves the citation. Pasting into a non-Gowiki target yields the resolved `[Author, Year]` text.

---

## 7. Permissions and config

- **Read access**: any user who can view the page can see the citations and references.
- **Edit access**: any user who can edit the page can add `{publication}` directives.
- **Admin**: cache management (list, force refresh) is admin-only.

Config block in `config.yaml`:

```yaml
bibliography:
  enabled: true
  pubmed_api_key: ""          # optional, raises rate limit to 10/s
  admin_contact_email: ""     # used in User-Agent for NIH etiquette
```

When `enabled: false`, directives render as `[citation disabled]` and no network calls are made.

---

## 8. Decisions

1. **Identifier-only model** — No free-form bibliography entries. Every citation must be resolvable via PMID or DOI. Keeps data clean, enables link-out, prevents drift between the inline display and the reference list.

2. **Author-year inline, author-year in list** — Numbered citations are common in physics and IEEE fields but are confusing to read and write in wiki prose. Author-year is the default; a numbered option may come in a later version.

3. **Hover = footnote-style popup** — Reuses existing frontend infrastructure and matches a reader expectation Gowiki already established for footnotes.

4. **Cache is permanent** — Metadata rarely changes; when it does, an admin can manually refresh. Avoids periodic re-fetches and network traffic.

5. **Automatic references section, overridable** — By default the references list is appended at page bottom. Placing `{references}` explicitly suppresses the auto-append and gives the author full control. This matches the way `{todo-list}` is positioned.

6. **One directive per citation** — Multiple-identifier syntax (`pmid="1,2,3"`) is rejected. Adjacent directives work fine and keep the markdown grep-able.

7. **DOI included in v1** — Preprints and non-indexed journals are common enough that PMID-only would be limiting from day one. Crossref's API is simple enough that this adds minimal implementation cost.

8. **PMID and DOI only — no arxiv/PMC/bookshelf in v1** — Those can be added incrementally, each with its own fetcher.
