package importer

import (
	"fmt"
	"sort"
	"strings"
)

// Markdown generates the import report as a Gowiki Markdown document.
func (r *Report) Markdown() string {
	var b strings.Builder

	b.WriteString("# Import Report\n\n")

	// Summary
	convPct := float64(0)
	if r.TotalLines > 0 {
		convPct = float64(r.ConvertLines) / float64(r.TotalLines) * 100
	}
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("| --- | --- |\n")
	fmt.Fprintf(&b, "| Total pages | %d |\n", r.TotalPages)
	fmt.Fprintf(&b, "| Total lines | %d |\n", r.TotalLines)
	fmt.Fprintf(&b, "| Converted lines | %d |\n", r.ConvertLines)
	fmt.Fprintf(&b, "| Flagged lines | %d |\n", r.FlaggedLines)
	fmt.Fprintf(&b, "| Conversion rate | %.1f%% |\n", convPct)
	fmt.Fprintf(&b, "| Media copied | %d |\n", r.MediaCopied)
	fmt.Fprintf(&b, "| Media missing | %d |\n", len(r.MediaMissing))
	b.WriteString("\n")

	// Unsupported features
	if len(r.Features) > 0 {
		b.WriteString("## Unsupported Features\n\n")
		b.WriteString("| Feature | Count |\n")
		b.WriteString("| --- | --- |\n")

		// Sort by count descending
		type kv struct {
			key   string
			count int
		}
		var sorted []kv
		for k, v := range r.Features {
			sorted = append(sorted, kv{k, v})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].count > sorted[j].count
		})
		for _, kv := range sorted {
			fmt.Fprintf(&b, "| %s | %d |\n", kv.key, kv.count)
		}
		b.WriteString("\n")
	}

	// Missing media
	if len(r.MediaMissing) > 0 {
		b.WriteString("## Missing Media\n\n")
		for _, m := range r.MediaMissing {
			fmt.Fprintf(&b, "- %s\n", m)
		}
		b.WriteString("\n")
	}

	// Per-page flagged lines
	flaggedPages := 0
	for _, p := range r.Pages {
		if len(p.Flagged) > 0 {
			flaggedPages++
		}
	}

	if flaggedPages > 0 {
		b.WriteString("## Flagged Pages\n\n")
		for _, p := range r.Pages {
			if len(p.Flagged) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### %s\n\n", p.SourcePath)
			for _, f := range p.Flagged {
				if f.LineNum > 0 {
					fmt.Fprintf(&b, "- Line %d: **%s** `%s`\n", f.LineNum, f.Reason, truncate(f.Content, 80))
				} else {
					fmt.Fprintf(&b, "- **%s** `%s`\n", f.Reason, truncate(f.Content, 80))
				}
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
