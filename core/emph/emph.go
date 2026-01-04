package emph

import (
	"fmt"
	"strings"

	"github.com/delahondes/gowiki/docmodel"
	"github.com/yuin/goldmark/ast"
)

const KindEmph docmodel.Kind = "emph"

type Emph struct {
	Content []docmodel.Node
}

func (n Emph) Kind() docmodel.Kind       { return KindEmph }
func (n Emph) Children() []docmodel.Node { return n.Content }

func init() {
	// Goldmark -> docmodel
	docmodel.RegisterGoldmarkImporter(ast.KindEmphasis,
		func(
			n ast.Node,
			source []byte,
			importChildren func(ast.Node, []byte) ([]docmodel.Node, error),
		) (docmodel.Node, error) {
			children, err := importChildren(n, source)
			if err != nil {
				return docmodel.Node{}, err
			}
			return docmodel.NewNode(KindEmph, struct{}{}, children), nil
		},
	)

	// Markdown
	docmodel.RegisterMarkdown(
		KindEmph,
		func() any { return struct{}{} },
		nil, // produced by parser, not standalone
		func(n docmodel.Node) (string, error) {
			var b strings.Builder
			b.WriteString("*")
			for _, c := range n.Children {
				s, _ := docmodel.EmitMarkdown(c)
				b.WriteString(s)
			}
			b.WriteString("*")
			return b.String(), nil
		},
	)

	// HTML
	docmodel.RegisterHTML(KindEmph, func(n docmodel.Node) (string, error) {
		var b strings.Builder
		b.WriteString("<em>")
		for _, c := range n.Children {
			s, _ := docmodel.RenderHTML(c)
			b.WriteString(s)
		}
		b.WriteString("</em>")
		return b.String(), nil
	})

	// Debug
	docmodel.RegisterDebug(KindEmph, func(n docmodel.Node, indent int) string {
		prefix := strings.Repeat("  ", indent)
		out := fmt.Sprintf("%sEMPH\n", prefix)
		for _, c := range n.Children {
			out += docmodel.Debug(c, indent+1)
		}
		if len(n.Children) == 0 {
			out += prefix + "  <no children>\n"
		}
		return out
	})
}
