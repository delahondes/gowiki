package mcpserver

// conventionsPayload returns the dialect rules and content guidelines AI
// agents must follow. The structure mirrors the HTTP API's /api/ai/v1/conventions
// response so clients that migrate from the HTTP API keep the same shape.
func conventionsPayload() map[string]any {
	return map[string]any{
		"dialect": map[string]any{
			"name":        "Gowiki Markdown",
			"description": "A bijective Markdown dialect. One canonical syntax per node type. Round-trip lossless.",
			"rules": []string{
				"*italic* only — _italic_ is NOT italic, it is underline",
				"**bold** only — __bold__ is rejected",
				"_underline_ — produces underline, NOT italic",
				"~~strikethrough~~",
				"~subscript~, ^superscript^",
				"ATX headings only (# H1, ## H2, etc.) — setext headings rejected",
				"- item for unordered lists — * is rejected as list marker",
				"1. item for ordered lists",
				"Raw HTML is forbidden — < and > are plain characters",
				"HTML entities are not interpreted — use UTF-8 directly",
				"Single newline in a paragraph = hard line break (<br>)",
				"Trailing spaces have no meaning — two-space line break rule does not exist",
				"\\n literal = explicit hard break (valid in lists and tables only, not in paragraphs)",
				"No column alignment syntax in tables",
				"Numbered headings use 1. prefix: ## 1. Title (not a property directive)",
				"^[inline footnote content] — supports inline markdown inside",
			},
			"forbidden": []string{
				"Do NOT use _text_ for italic — it means underline",
				"Do NOT use __text__ for bold",
				"Do NOT use * as a list marker",
				"Do NOT use raw HTML tags",
				"Do NOT use HTML entities (e.g. &amp;) — use the UTF-8 character directly",
				"Do NOT use setext headings (underline-style)",
				"Do NOT use trailing spaces for line breaks",
				"Do NOT use multi-body tables (<tbody>)",
			},
			"directives": map[string]string{
				"syntax":             "{directivename key=value key2=\"value with spaces\"} on its own line",
				"self_contained":     "{reviewflow-link version=2.0} — stands alone as its own node",
				"prefix":             "{image size=500px} followed by the target block on the next line",
				"properties_example": "{reviewflow version=1.0 author=alice reviewer=bob}",
			},
		},
		"content_rules": map[string]any{
			"page_links": []string{
				"/path/to/page → content/path/to/page.md",
				"/path/to/namespace/ → content/path/to/namespace/index.md",
				"./page → adjacent page.md relative to current page",
			},
			"attachment_links": []string{
				"Attachments must have a file extension",
				"/path/to/file.ext → content/path/to/file.ext",
				"./page.md is the raw attachment; ./page is the rendered page",
			},
			"namespace_constraint": "If content/path/to/ns/ directory exists, content/path/to/ns.md must NOT exist",
			"metadata_location":    "data/meta/ mirrors content/ structure, with .json extension instead of .md",
		},
		"conventions": map[string]any{
			"summary_format":     "[AI: <tool_name>] <description of change>",
			"summary_example":    "[AI: Claude] Translate section 3 to English",
			"summary_required":   "Summary is required for all write operations (write_page tool)",
			"optimistic_locking": "Always read the page first, then write with expected_version set to the version you read",
			"tools_guidance":     "Use read_pages_batch for multi-page reads; preview_page_diff before write_page; call get_conventions once at session start",
		},
		"quality_checks": map[string]any{
			"use_preview":       "Always call preview_page_diff before write_page and show the diff to the user",
			"read_before_write": "Read the page with read_pages_batch immediately before preview to get its current version for expected_version",
		},
		"fenced_blocks": map[string]any{
			"description": "Fenced code blocks (```lang) render as syntax-highlighted source. Two info-string values render as embedded visualizations instead: `mermaid` and `chart`. Both are rendered natively by the wiki — no external assets, no attachments needed. Prefer them over uploading rendered PNGs when the diagram or plot is data you can express as text.",
			"code": map[string]any{
				"syntax":  "```<language>\\n<source>\\n```",
				"note":    "Language name enables highlighting. Use it for real source code — not for diagrams that mermaid or chart can render.",
				"example": "```go\\nfmt.Println(\"hello\")\\n```",
			},
			"mermaid": map[string]any{
				"syntax":       "```mermaid [size=<CSSlength>] [caption=\"<text>\"]\\n<mermaid source>\\n```",
				"library":      "Mermaid 11 (loaded on demand from a CDN). Supports flowchart, sequenceDiagram, classDiagram, stateDiagram, erDiagram, gantt, pie, journey, timeline, mindmap, quadrantChart, gitGraph.",
				"info_string":  "size accepts any CSS length (e.g. 500px, 80%). caption becomes a rendered caption below the diagram. Both optional.",
				"example":      "```mermaid caption=\"State machine\"\\nstateDiagram-v2\\n  [*] --> Draft\\n  Draft --> Review\\n  Review --> Published\\n```",
				"when_to_use":  "Any diagram whose structure is describable as text — flow, states, sequences, dependencies. Renders live in the visual editor and in view mode.",
			},
			"chart": map[string]any{
				"syntax":      "```chart <type> [<W>x<H>] [\"<title>\"] [nolegend|legend] [values] [left|right] [#RRGGBB #RRGGBB ...]\\n<label> = <number>\\n<label> = <number>\\n# comment lines allowed\\n```",
				"types":       []string{"pie", "doughnut", "bar", "hbar", "line", "radar", "polar"},
				"library":     "Chart.js (loaded on demand). Rendered natively — no image upload.",
				"defaults":    "400x250, legend on, values off, center-aligned, default colour palette. Body: one `label = value` pair per line; `#` starts a comment.",
				"example":     "```chart bar 500x300 \"Sales by quarter\" values\\nQ1 = 12\\nQ2 = 19\\nQ3 = 15\\nQ4 = 22\\n```",
				"when_to_use": "Small quantitative charts (fewer than ~30 data points). For anything with axes/annotations/multiple series that Chart.js can't express, generate an image instead and use upload_attachment_instructions.",
			},
		},
		"do_not": []string{
			"Do not introduce alternative Markdown syntaxes — bijectivity is non-negotiable",
			"Do not store metadata under data/content/",
			"Do not create extension-less files under data/content/",
			"Do not create content/path/to/ns.md if content/path/to/ns/ exists",
			"Do not silently change document structure — preserve existing formatting",
			"Do not remove or reformat content you were not asked to change",
		},
	}
}
