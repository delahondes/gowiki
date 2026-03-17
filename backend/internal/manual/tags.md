# Tags

Tags let you categorize pages and query them across the wiki.

## 1. Tagging a page

Add a tag directive anywhere in the page (typically at the top):

```
{tag sop}
```

Multiple tags can be assigned by using multiple directives:

```
{tag sop}
{tag quality}
```

## 1. Tag queries

Display a table of all pages with a given tag using a tag query:

```
{tag-query tag=sop}
```

The query renders a table with columns: Page, Version, Date, Author.

Optional parameters:
- `path=/regulatory/qms` — restrict to a namespace
- `exclude=draft,archived` — exclude pages that also have these tags

## 1. Reviewflow integration

For pages with reviewflow, the tag query table shows the latest validated version tag as a clickable link that navigates to the archived validated version.
