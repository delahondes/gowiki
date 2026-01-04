package paragraph

import (
	"fmt"
	"strings"

	"github.com/delahondes/gowiki/docmodel"
	"github.com/yuin/goldmark/ast"
)

const KindParagraph docmodel.Kind = "paragraph"

func init() {
	// Goldmark -> docmodel
	docmodel.RegisterGoldmarkImporter(ast.KindParagraph,
		func(
			n ast.Node,
			source []byte,
			importChildren func(ast.Node, []byte) ([]docmodel.Node, error),
		) (docmodel.Node, error) {
			children, err := importChildren(n, source)
			if err != nil {
				return docmodel.Node{}, err
			}
			return docmodel.NewNode(KindParagraph, struct{}{}, children), nil
		},
	)

	docmodel.RegisterMarkdown(
		KindParagraph,
		func() any { return struct{}{} },
		nil,
		func(n docmodel.Node) (string, error) {
			var b strings.Builder
			for _, c := range n.Children {
				s, _ := docmodel.EmitMarkdown(c)
				b.WriteString(s)
			}
			return b.String() + "\n\n", nil
		},
	)
	docmodel.RegisterHTML(KindParagraph, func(n docmodel.Node) (string, error) {
		var b strings.Builder
		b.WriteString("<p>")
		for _, c := range n.Children {
			s, _ := docmodel.RenderHTML(c)
			b.WriteString(s)
		}
		b.WriteString("</p>")
		return b.String(), nil
	})
	docmodel.RegisterDebug(KindParagraph, func(n docmodel.Node, indent int) string {
		prefix := strings.Repeat("  ", indent)
		out := fmt.Sprintf("%sPARAGRAPH\n", prefix)
		for _, c := range n.Children {
			out += docmodel.Debug(c, indent+1)
		}
		return out
	})

}
