package docmodel

import (
	"fmt"
	"strings"
)

func EmitMarkdown(n Node) (string, error) {
	if IsZeroNode(n) {
		return "", nil
	}
	emitter, ok := mdEmitters[n.Kind]
	if !ok {
		return "", fmt.Errorf("no markdown emitter for kind %q", n.Kind)
	}
	return emitter(n)
}

func RenderHTML(n Node) (string, error) {
	if IsZeroNode(n) {
		return "", nil
	}
	renderer, ok := htmlRenders[n.Kind]
	if !ok {
		return "", fmt.Errorf("no html renderer for kind %q", n.Kind)
	}
	return renderer(n)
}

func Debug(n Node, indent int) string {
	if IsZeroNode(n) {
		return strings.Repeat("  ", indent) + "<nil>\n"
	}
	if r, ok := debugRenders[n.Kind]; ok {
		return r(n, indent)
	}

	// fallback: kind + children
	prefix := strings.Repeat("  ", indent)
	out := prefix + string(n.Kind) + "\n"
	for _, c := range n.Children {
		out += Debug(c, indent+1)
	}
	if len(n.Children) == 0 {
		out += prefix + "  <no children>\n"
	}
	return out
}
