package docmodel

import (
	"strings"
)

func init() {
	// Markdown emitter
	RegisterMarkdown(KindDocument, func() any { return struct{}{} }, nil, func(n Node) (string, error) {
		var parts []string
		for _, c := range n.Children {
			s, err := EmitMarkdown(c)
			if err != nil {
				return "", err
			}
			if s != "" {
				parts = append(parts, s)
			}
		}
		// Markdown documents are separated by blank lines
		return strings.Join(parts, "\n\n"), nil
	})

	// HTML renderer
	RegisterHTML(KindDocument, func(n Node) (string, error) {
		var b strings.Builder
		for _, c := range n.Children {
			s, err := RenderHTML(c)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
		return b.String(), nil
	})

	// Debug renderer
	RegisterDebug(KindDocument, func(n Node, indent int) string {
		prefix := strings.Repeat("  ", indent)
		out := prefix + "document\n"
		for _, c := range n.Children {
			out += Debug(c, indent+1)
		}
		if len(n.Children) == 0 {
			out += prefix + "  <empty>\n"
		}
		return out
	})
}
