# Links

## 1. Internal page links

```
[Display text](/path/to/page)
```

If the label is empty, the page name is displayed automatically:

```
[](/path/to/page)
```

Relative links are supported:

```
[Sibling page](./other-page)
```

## 1. External links

```
[Example](https://example.com)
```

External links display with a distinct icon and open in a new tab.

## 1. Email links

```
[](mailto:user@example.com)
```

Bare email addresses are auto-linked.

## 1. Media / attachment links

Links to files with extensions are treated as attachment downloads:

```
[Download](./report.pdf)
[](./data.csv)
```

The file type icon is displayed automatically.

## 1. Link resolution rules

- `/path/to/page` resolves to `content/path/to/page.md`
- `/path/to/namespace/` resolves to `content/path/to/namespace/index.md`
- `./page` resolves relative to the current page
- `./page.md` is the raw attachment (the `.md` file itself), not the rendered page
