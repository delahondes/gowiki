package api

import (
	"fmt"
	"path"
	"strings"

	"gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
)

// titleFromPagePath derives a human-readable title from a page path
// leaf. "/foo/bar" → "bar"; "/foo/bar/" → "bar"; "/" → "index". The
// _template.md draft prefill uses it so {template-title}'s pattern
// heading is substituted even when the caller didn't yet pass a
// title (the user can rename inside the editor before saving).
func titleFromPagePath(pagePath string) string {
	trimmed := strings.TrimSpace(pagePath)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || trimmed == "/" {
		return "index"
	}
	return path.Base(trimmed)
}

// templateResolveResult carries everything a caller needs after running
// a template → document conversion: the finished markdown, the stamp
// args the resolver stamped (so the caller can echo them back), and the
// merged reviewflow args (nil when the created doc carries no reviewflow).
type templateResolveResult struct {
	Markdown       string
	Stamp          markdown.TemplateStampArgs
	ReviewflowArgs map[string]string
}

// resolveTemplateDoc runs the full template → document pipeline that
// both the explicit Create-from-template action and the _template.md
// draft prefill use:
//
//  1. refuse when the source has no {template} marker (not_template)
//  2. enforce the {template target=…} pattern against dstPath, when set
//     (invalid_target)
//  3. refuse when the template owns a {reviewflow} and it isn't fully
//     validated (not_validated) — stops an un-reviewed template from
//     being used, however the caller found it
//  4. split off the payload below the {template} marker
//  5. merge reviewflow args (overrides > {template-reviewflow} args >
//     template's own {reviewflow} args)
//  6. compose the stamp
//  7. resolve the payload (title substitution, {template-stamp} →
//     stamp sentence, {template-reviewflow} → {reviewflow}, {template-todo}
//     → {todo}, {template-title} marker drop)
//
// The caller handles destination-exists checks, ACL, and the actual
// write — this helper is purely a markdown transform plus guards.
//
// dstPath is the canonical path of the page being created (used for the
// target-pattern check); when the caller doesn't yet know the final
// destination (speculative prefill of a draft the user may rename), it
// passes dstPath=="" and the target-pattern check is skipped. title
// drives {template-title} substitution; empty means "leave the pattern
// heading alone, just drop the marker".
func (s *Server) resolveTemplateDoc(
	tplStorage string,
	tplMarkdown string,
	tplVersion int64,
	dstPath string,
	title string,
	overrides map[string]string,
) (*templateResolveResult, *TemplateCreateError) {
	// 1. Must carry a template-family directive. {template} OR any
	// {template-*} directive both qualify — the row-bound-template
	// convention is to skip {template} on the template file (otherwise
	// the Create button on it would mislead the user into creating
	// rows outside the database), and we still want {template-title},
	// {template-stamp}, {template-reviewflow}, {template-todo} to
	// resolve on each row-bound page's prefill.
	if !markdown.HasAnyTemplateDirective(tplMarkdown) {
		return nil, &TemplateCreateError{
			Kind:    "not_template",
			Message: fmt.Sprintf("%s has no template directive ({template} or any {template-*})", tplStorage),
		}
	}

	// 2. Target pattern enforcement. Only when dstPath is supplied —
	// the prefill path leaves it empty when the user hasn't committed
	// to the final destination yet, and we don't want to refuse a
	// first-save that the user is about to rename anyway.
	if dstPath != "" {
		if targetPattern := markdown.TemplateTargetPattern(tplMarkdown); targetPattern != "" {
			wantPath := strings.TrimSpace(markdown.ResolveTemplateTargetPattern(targetPattern, title))
			if wantPath != "" {
				if !strings.HasPrefix(wantPath, "/") {
					wantPath = "/" + wantPath
				}
				if wantPath != dstPath {
					return nil, &TemplateCreateError{
						Kind:    "invalid_target",
						Message: fmt.Sprintf("template %s pins its target to %s (from `target=%s`); caller asked for %s", tplStorage, wantPath, targetPattern, dstPath),
					}
				}
			}
		}
	}

	// 3. Reviewflow validation gate. A template with no reviewflow is
	// legitimate (spec §5): skip the check and stamp with the plain
	// page version.
	templateOwnRF := markdown.ParseReviewflowArgs(tplMarkdown)
	hasReviewflow := len(templateOwnRF) > 0
	versionTag := ""
	if hasReviewflow {
		if s.reviewflowService == nil {
			return nil, &TemplateCreateError{Kind: "not_validated", Message: "reviewflow service unavailable"}
		}
		status, rfErr := s.reviewflowService.GetStatus(tplStorage)
		if rfErr == nil && status != nil {
			if !status.IsFullyValidated {
				missing := sortedRoles(status.MissingRoles)
				return nil, &TemplateCreateError{
					Kind:    "not_validated",
					Message: fmt.Sprintf("template %s is not fully validated — missing role(s): %s. Complete the review before issuing documents from it.", tplStorage, strings.Join(missing, ", ")),
				}
			}
			versionTag = status.VersionTag
		}
	}

	// 4. Split payload. With {template} present, the content above the
	// marker is tracking metadata (reviewflow, tags on the template
	// page itself) and only the content below is the payload. Without
	// {template} — the row-bound-template convention — the file IS the
	// payload, same shape SplitTemplatePayload would return for a
	// marker on line 1.
	_, payload, ok := markdown.SplitTemplatePayload(tplMarkdown)
	if !ok {
		payload = tplMarkdown
	}

	// 5. Merge reviewflow args. Only produce args when the template
	// actually carries {template-reviewflow} OR when the caller passed
	// overrides. If neither, the created document has no reviewflow —
	// the template may be one that doesn't want to require review.
	tplRF, tplRFPresent := markdown.ParseTemplateReviewflowArgs(payload)
	var mergedRF map[string]string
	if tplRFPresent || len(overrides) > 0 {
		mergedRF = markdown.MergeReviewflowArgs(overrides, tplRF, templateOwnRF)
	}

	// 6. Compose the stamp.
	stamp := markdown.TemplateStampArgs{
		TemplatePath:        storage.CanonicalPath(tplStorage),
		TemplateTitle:       markdown.ExtractTitle(tplMarkdown),
		TemplatePageVersion: tplVersion,
		VersionTag:          versionTag,
	}

	// 7. Resolve.
	resolved := markdown.ResolveTemplatePayload(payload, markdown.TemplateResolveOpts{
		Stamp:          stamp,
		Title:          title,
		ReviewflowArgs: mergedRF,
	})

	return &templateResolveResult{
		Markdown:       resolved,
		Stamp:          stamp,
		ReviewflowArgs: mergedRF,
	}, nil
}
