There is an issue with internal links and media attachment that we did not foresee: they are the same, notably in markdown syntax. So we cannot have rendering rule that uses different root without introducing ambiguous syntaxic rules. The sane conclusion is that all content should live under the same root ('content' mixing previous pages and media content). 

So these would be internal links:
- `[link](/path/to/page)` would designate the internal link to the page rendered from content/path/to/page.md
- `[link](/path/to/namespace)` or `[link](/path/to/namespace/)` would designate the internal link to the page rendered from content/path/to/namespace/index.md (which means it's forbidden to have content/path/to/namespace.md if content/path/to/namespace/ folder exists, something we already said in general specs)
- `[link](./page)` would be a local link to an adjacent page renderd from relative ./page.md from current page path. 
- and similarly for relative namespace links.

And these would be attachments:
- `[filename](/path/to/attachment.ext)` would designate the attachment link to the media content/path/to/attachment.ext
- `[filename](./attachment.ext)` would designate the relative attachment link to adjacent media attachment.ext

Note that this introduce a subtlety, but which causes no syntaxic issue:
- `[link](./page)` is a link to render page.md
- `[link](./page.md)` is the attachment of the same page.md source.

Which means we forbid media files without extension to remove any ambiguity.

Also currently we mix content and meta information:
```
.
├── home.md
├── home.meta.json
├── index.md
└── index.meta.json
```
This is not correct, it introduces the risk of collision with attachment and it is easy to avoid.

So we should have two root in data: content and meta. content should host all pages (.md files and attachments). meta should mirror the folder organisation of content but hold meta.json files.
There is an issue with internal links and media attachments that we did not foresee: in Markdown, **page links and download links use the same syntax** (`[text](target)`). Therefore we cannot apply different resolution roots ("pages" vs "media") without introducing ambiguous, non-standard rules.

The sane conclusion is that **all user content lives under the same root** (`content/`), mixing what we previously called pages and media attachments.

## Content layout

All user-managed files live under:
- `data/content/` 

System metadata must not share the same namespace as user content. Therefore metadata lives under:
- `data/meta/`, mirroring the folder organisation of `content/`.

## Link resolution rules

### Rendered page links (internal wiki links)

These are links that resolve to a **rendered page**.

- `[link](/path/to/page)` designates the internal link to the page rendered from `content/path/to/page.md`.

- `[link](/path/to/namespace)` and `[link](/path/to/namespace/)` designate the internal link to the page rendered from `content/path/to/namespace/index.md`.
  - This implies the usual constraint: if `content/path/to/namespace/` exists, then `content/path/to/namespace.md` must not exist.

- `[link](./page)` is a local link to an adjacent page rendered from `./page.md` relative to the current page path.

- The same applies for relative namespace links, e.g. `[link](./ns/)` resolving to `./ns/index.md`.

### Attachments (download links)

These are links that resolve to a **raw file download / direct media serving**.

- `[filename](/path/to/attachment.ext)` designates an attachment served from `content/path/to/attachment.ext`.

- `[filename](./attachment.ext)` designates a relative attachment link to `./attachment.ext`.

To remove any ambiguity, we **forbid attachments without an extension** (no `content/**/file` without `.ext`).

### Subtle but intentional: `.md` can be both a page source and an attachment

This introduces a subtlety, but it causes no syntax issue and is clear from a user perspective:

- `[link](./page)` is a link to the **rendered page** `page.md`.
- `[link](./page.md)` is a link to the **raw attachment** `page.md` source.

Therefore we do **not** forbid `.md` as an attachment type.

## Metadata layout

We must not mix content and metadata files in the same directory tree.

The following is **not** correct (risk of collision with attachments):

```text
.
├── home.md
├── home.meta.json
├── index.md
└── index.meta.json
```

Instead, metadata lives under `meta/` with a structure mirroring `content/`.

Example:

```text
data/
├── content/
│   ├── home.md
│   ├── SCR-20260211-qwuq.png
│   └── docs/
│       └── index.md
└── meta/
    ├── home.json
    └── docs/
        └── index.json
```

Notes:
- Metadata filenames are plain `.json` under `meta/` (no `.meta.json` suffix is needed once separated).
- The metadata file path is derived from the page path by:
  - replacing the `content/` root with `meta/`
  - replacing the `.md` extension with `.json`