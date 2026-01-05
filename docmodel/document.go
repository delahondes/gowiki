package docmodel

import (
	"strings"
)

func toMarkdown(n Node) (string, error) {
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
	return strings.Join(parts, ""), nil
}

func renderHTML(n Node) (string, error) {
	var b strings.Builder
	for _, c := range n.Children {
		s, err := RenderHTML(c)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

func init() {
	// Markdown emitter
	RegisterMarkdown(KindDocument, func() any { return struct{}{} }, nil, toMarkdown)
	RegisterMarkdown(KindFragment, func() any { return struct{}{} }, nil, toMarkdown)

	// HTML renderer
	RegisterHTML(KindDocument, renderHTML)
	RegisterHTML(KindFragment, renderHTML)

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
	RegisterDebug(KindFragment, func(n Node, indent int) string {
		prefix := strings.Repeat("  ", indent)
		out := prefix + "fragment\n"
		for _, c := range n.Children {
			out += Debug(c, indent+1)
		}
		if len(n.Children) == 0 {
			out += prefix + "  <empty>\n"
		}
		return out
	})
}
