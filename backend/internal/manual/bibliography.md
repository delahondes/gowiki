# Bibliography

The bibliography plugin lets you cite scientific publications by their PubMed ID or DOI, with hover previews and a References section auto-generated at the bottom of the page.

## 1. Citing a publication

Insert a `{publication}` directive anywhere inline:

```markdown
The gut microbiome predicts response to ICI therapy {publication pmid=38480887}.
Preprints are supported too {publication doi="10.1101/2024.02.15.580456"}.
```

Exactly one of `pmid` or `doi` must be provided.

**What you get:**

- The inline marker renders as `[Author, Year]` — e.g. `[Derosa et al., 2024]`. Hovering shows a popup with the title, full author list, journal, and year.
- Clicking opens PubMed (for PMIDs) or the DOI resolver (`https://doi.org/...`) in a new tab.
- The same entry automatically appears in the References section at the bottom of the page.

## 2. The References section

By default, the plugin appends a **References** heading plus a sorted reference list at the bottom of every page that contains at least one `{publication}` directive. Entries are sorted alphabetically by first-author family name.

To place the list somewhere else, insert a `{references}` directive at the desired location:

```markdown
## Further reading

{references}
```

When a `{references}` directive is present, the automatic append at page bottom is skipped.

### Options

| Attribute | Default | Purpose |
| --- | --- | --- |
| `title` | `References` | Heading text for the section. |
| `show_heading` | `true` | Set to `false` to render just the list, with no heading. |

```markdown
{references title="Sources" show_heading=false}
```

## 3. Metadata caching

The first time a PMID or DOI is referenced, the server fetches its metadata from **PubMed** (via NCBI E-utilities) or **Crossref** (for DOIs) and caches it permanently under `data/meta/bibliography/`. Subsequent page renders are instant and work offline.

If the source API is temporarily unavailable, the citation shows as `[pmid:... ⚠]` with a tooltip. Re-rendering the page after the source recovers resolves the citation.

If a PMID or DOI is malformed (or not found at PubMed / Crossref), the inline marker shows an error badge and the reference list entry reads "Invalid …" or "Unresolved …".

## 4. Insert buttons

The editor toolbar gets two new buttons when the plugin is active:

- **Insert citation** — inserts a `{publication}` directive; opens the properties panel so you can paste a PMID or DOI.
- **Insert references list** — inserts a `{references}` directive so you can control its placement.

## 5. Admin configuration

The plugin is enabled in `config.yaml`:

```yaml
bibliography:
  enabled: true
  pubmed_api_key: ""          # optional — raises PubMed rate limit from 3/s to 10/s
  admin_contact_email: ""     # included in outbound User-Agent per NIH etiquette
```

A PubMed API key is free (see [NCBI API Key Management](https://support.nlm.nih.gov/kbArticle/?pn=KA-05317)) and only useful if your wiki issues many first-time lookups. Normal interactive usage stays well under the 3/s unauthenticated limit.

Crossref (DOI) needs no key — a polite `User-Agent` with a contact email is enough, which is why `admin_contact_email` is recommended when enabling the plugin.
