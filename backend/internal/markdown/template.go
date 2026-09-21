package markdown

import (
	"fmt"
	"regexp"
	"strings"
)

// ── Template directive machinery ─────────────────────────────────────────
//
// A "template" page is one that carries a {template} directive. That
// directive marks the boundary between the template's tracking block
// (above) and the copiable payload (below). At document-creation time the
// wiki reads the payload, resolves three directives inside it, and writes
// the result to a new page:
//
//   {template-title}      → dropped; the heading that follows is replaced
//                           with the user-supplied title
//   {template-stamp}      → replaced with the "Created from template …"
//                           sentence (link + frozen template version)
//   {template-reviewflow} → replaced with {reviewflow …}
//
// This file holds the pure text-level helpers. The higher-level orchestration
// (permission checks, reviewflow validation, PageStore.Put) lives in
// internal/api/template_create.go.

var (
	// templateMarkerRe matches a {template} directive line — the marker that
	// separates the tracking block from the copiable payload. No args yet
	// but the directive family may grow.
	templateMarkerRe = regexp.MustCompile(`^\s*\{template(?:\s[^{}]*)?\}\s*$`)

	// templateTitleRe matches a {template-title} directive line. The
	// directive is a marker prefix on the next heading — this line is
	// dropped at creation time and the heading below is rewritten.
	templateTitleRe = regexp.MustCompile(`^\s*\{template-title(?:\s[^{}]*)?\}\s*$`)

	// templateStampRe matches a {template-stamp} directive line.
	templateStampRe = regexp.MustCompile(`^\s*\{template-stamp(?:\s[^{}]*)?\}\s*$`)

	// templateReviewflowRe matches a {template-reviewflow …} directive
	// line. Args mirror {reviewflow}: version, author, reviewer, validation.
	templateReviewflowRe = regexp.MustCompile(`^\s*\{template-reviewflow(\s[^{}]*)?\}\s*$`)

	// reviewflowRe matches the template's own {reviewflow …} directive.
	// Used to lift default actors when {template-reviewflow} omits them.
	reviewflowRe = regexp.MustCompile(`^\s*\{reviewflow(\s[^{}]*)?\}\s*$`)

	// atxHeadingRe matches an ATX heading — the payload's title line.
	atxHeadingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

	// directiveKVRe matches key=value or key="quoted value" pairs inside a
	// directive arg list. Used to parse both {template-reviewflow} args and
	// {reviewflow} args when we lift template defaults.
	directiveKVRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)\s*=\s*(?:"([^"]*)"|(\S+))`)
)

// IsTemplatePage returns true when the page's markdown contains a
// {template} directive. Only the marker matters; the tracking-block content
// above it is not inspected.
func IsTemplatePage(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if templateMarkerRe.MatchString(line) {
			return true
		}
	}
	return false
}

// SplitTemplatePayload returns the tracking block (above {template}) and
// the copiable payload (below it), minus the marker line itself. Any blank
// lines that immediately followed the marker are stripped from the payload
// so the created document doesn't start with an empty line (a template
// naturally has a blank line between {template} and the first payload
// directive). When the page has no {template} marker the second return
// value is empty and ok is false.
func SplitTemplatePayload(content string) (header, payload string, ok bool) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if templateMarkerRe.MatchString(line) {
			header = strings.Join(lines[:i], "\n")
			rest := lines[i+1:]
			// Trim leading blank lines from the payload.
			for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
				rest = rest[1:]
			}
			payload = strings.Join(rest, "\n")
			return header, payload, true
		}
	}
	return content, "", false
}

// ParseTemplateReviewflowArgs parses the args of a {template-reviewflow}
// directive line, returning a map of arg → value. Missing directive line
// returns an empty map.
func ParseTemplateReviewflowArgs(content string) (map[string]string, bool) {
	for _, line := range strings.Split(content, "\n") {
		m := templateReviewflowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return parseDirectiveKVs(m[1]), true
	}
	return nil, false
}

// ParseReviewflowArgs parses the FIRST {reviewflow …} directive line in
// content, returning the arg map (version, author, reviewer, validation …).
// Empty when there is no reviewflow directive.
func ParseReviewflowArgs(content string) map[string]string {
	for _, line := range strings.Split(content, "\n") {
		m := reviewflowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return parseDirectiveKVs(m[1])
	}
	return nil
}

func parseDirectiveKVs(args string) map[string]string {
	out := make(map[string]string)
	if args == "" {
		return out
	}
	for _, m := range directiveKVRe.FindAllStringSubmatch(args, -1) {
		v := m[2]
		if v == "" {
			v = m[3]
		}
		out[m[1]] = v
	}
	return out
}

// TemplateStampArgs holds the frozen origin data written into a document
// by {template-stamp}. Everything is computed once, at creation time, and
// never resolved again.
type TemplateStampArgs struct {
	// TemplatePath is the canonical path of the source template (leading
	// slash, no trailing /index).
	TemplatePath string
	// TemplateTitle is the H1 title of the source template — displayed as
	// the link's visible text.
	TemplateTitle string
	// TemplatePageVersion is the wiki's revision counter for the template
	// at the moment of the copy, used as the ?v=N pin.
	TemplatePageVersion int64
	// VersionTag is the reviewflow VERSIONTAG for the template — the
	// human-facing version (e.g. "1.0"). Empty when the template has no
	// reviewflow; the sentence then reads "revision <N>" instead of
	// "version <tag>".
	VersionTag string
}

// FormatTemplateStamp renders the origin sentence. When VersionTag is
// non-empty it reads "…, version <tag>"; otherwise "…, revision <N>". The
// wording matters: the two numbers name different things and should not
// look alike (spec §2 template-stamp).
func FormatTemplateStamp(args TemplateStampArgs) string {
	label := args.TemplateTitle
	if label == "" {
		label = args.TemplatePath
	}
	linkHref := args.TemplatePath
	if args.TemplatePageVersion > 0 {
		linkHref = fmt.Sprintf("%s?v=%d", args.TemplatePath, args.TemplatePageVersion)
	}
	versionPart := ""
	if args.VersionTag != "" {
		versionPart = fmt.Sprintf(", version %s", args.VersionTag)
	} else if args.TemplatePageVersion > 0 {
		versionPart = fmt.Sprintf(", revision %d", args.TemplatePageVersion)
	}
	return fmt.Sprintf("Created from template [%s](%s)%s", label, linkHref, versionPart)
}

// TemplateResolveOpts captures every input the resolver needs to produce a
// created-document markdown from a template payload.
type TemplateResolveOpts struct {
	// Stamp is the origin sentence data — the resolver writes the sentence
	// into every {template-stamp} line.
	Stamp TemplateStampArgs
	// Title replaces the pattern heading that follows {template-title}.
	// When empty the pattern is left alone and only the {template-title}
	// marker is removed (defensive: the create-from-template action always
	// passes a title, but the row-bound-page path may not).
	Title string
	// ReviewflowArgs is the resolved arg map for {template-reviewflow}. The
	// caller assembles it by merging create-action overrides with the
	// template's {template-reviewflow} args and, for any still-missing
	// keys, the template's own {reviewflow} directive. When nil the
	// {template-reviewflow} line is dropped without emitting a {reviewflow}
	// (row-bound-page path).
	ReviewflowArgs map[string]string
}

// ResolveTemplatePayload rewrites a template's payload into the markdown
// of the created document. Every {template-*} directive is either removed
// or replaced; everything else is preserved byte-for-byte (including code
// fences, tables, and the {tag rec} block that opens the payload).
//
// The resolver operates line-by-line on the raw text. Round-trip safety
// isn't a goal here — the output is a fresh document, not a re-serialization.
func ResolveTemplatePayload(payload string, opts TemplateResolveOpts) string {
	lines := strings.Split(payload, "\n")
	out := make([]string, 0, len(lines))
	stamp := FormatTemplateStamp(opts.Stamp)

	i := 0
	for i < len(lines) {
		line := lines[i]

		// {template-title} — drop the directive line, then, when a title
		// override is supplied, replace the very next non-empty heading
		// line's text (keeping its `#` prefix).
		if templateTitleRe.MatchString(line) {
			if opts.Title != "" {
				j := i + 1
				for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
					out = append(out, lines[j])
					j++
				}
				if j < len(lines) {
					if hm := atxHeadingRe.FindStringSubmatch(lines[j]); hm != nil {
						lines[j] = hm[1] + " " + opts.Title
					}
				}
				i++
				continue
			}
			// No title override — just drop the marker.
			i++
			continue
		}

		// {template-stamp}
		if templateStampRe.MatchString(line) {
			out = append(out, stamp)
			i++
			continue
		}

		// {template-reviewflow …}
		if templateReviewflowRe.MatchString(line) {
			if opts.ReviewflowArgs != nil {
				out = append(out, renderReviewflowDirective(opts.ReviewflowArgs))
			}
			// else: drop it silently — row-bound-page path.
			i++
			continue
		}

		out = append(out, line)
		i++
	}
	return strings.Join(out, "\n")
}

// renderReviewflowDirective writes a {reviewflow …} directive from an arg
// map. Version comes first, then roles alphabetically — mirrors the
// frontend's serializer order so a page authored by hand and a page
// produced from a template look identical.
func renderReviewflowDirective(args map[string]string) string {
	var parts []string
	if v, ok := args["version"]; ok && v != "" {
		parts = append(parts, "version="+v)
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k == "version" {
			continue
		}
		keys = append(keys, k)
	}
	// Alphabetical, stable.
	sortStrings(keys)
	for _, k := range keys {
		if v := args[k]; v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		return "{reviewflow}"
	}
	return "{reviewflow " + strings.Join(parts, " ") + "}"
}

// MergeReviewflowArgs computes the final {reviewflow} args for a created
// document. Precedence (highest first):
//   1. create-action overrides (from the dialog / MCP tool call)
//   2. {template-reviewflow} args in the template's payload
//   3. the template's own {reviewflow} directive (its actors)
//   4. defaults: version = "1.0"
//
// A caller can force a key to empty by passing "" in overrides — that keeps
// the merge deterministic. Unknown keys pass through untouched.
func MergeReviewflowArgs(overrides, templateReviewflow, templateOwn map[string]string) map[string]string {
	result := make(map[string]string)
	// Seed with template's own values (excluding version — a template's
	// own version is its own state, not the created document's starting
	// version).
	for k, v := range templateOwn {
		if k == "version" {
			continue
		}
		result[k] = v
	}
	// Layer template-reviewflow args on top.
	for k, v := range templateReviewflow {
		result[k] = v
	}
	// Then create-action overrides.
	for k, v := range overrides {
		result[k] = v
	}
	if _, ok := result["version"]; !ok || result["version"] == "" {
		result["version"] = "1.0"
	}
	return result
}

// ExtractTemplateTitlePattern returns the pattern-text of the heading that
// follows the first {template-title} directive in the payload, without any
// leading `#` characters. Callers use it to prefill the Create document
// dialog. Returns "" when no {template-title} is present or the next line
// isn't a heading.
func ExtractTemplateTitlePattern(payload string) string {
	lines := strings.Split(payload, "\n")
	for i, line := range lines {
		if !templateTitleRe.MatchString(line) {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j < len(lines) {
			if hm := atxHeadingRe.FindStringSubmatch(lines[j]); hm != nil {
				return hm[2]
			}
		}
		return ""
	}
	return ""
}

// sortStrings is a small inline helper so callers don't have to import
// "sort" from every use site.
func sortStrings(s []string) {
	// Insertion sort — the arg list is always tiny (<10 keys); avoids
	// pulling in sort just for this.
	for i := 1; i < len(s); i++ {
		v := s[i]
		j := i - 1
		for j >= 0 && s[j] > v {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = v
	}
}
