package text

import (
	"fmt"
	"strings"

	"github.com/delahondes/gowiki/docmodel"
	"github.com/yuin/goldmark/ast"
)

const KindText docmodel.Kind = "text"

type Payload struct {
	Value string
}

func init() {
	docmodel.RegisterMarkdown(
		KindText,
		func() any { return Payload{} },
		nil, // text is produced by others, not parsed alone
		func(n docmodel.Node) (string, error) {
			return n.Payload.(Payload).Value, nil
		},
	)
	docmodel.RegisterHTML(KindText, func(n docmodel.Node) (string, error) {
		return n.Payload.(Payload).Value, nil
	})
	docmodel.RegisterDebug(KindText, func(n docmodel.Node, indent int) string {
		return fmt.Sprintf("%sTEXT(%q)\n",
			strings.Repeat("  ", indent),
			n.Payload.(Payload).Value,
		)
	})
	docmodel.RegisterGoldmarkImporter(ast.KindText,
		func(
			n ast.Node,
			source []byte,
			importChildren func(ast.Node, []byte) ([]docmodel.Node, error),
		) (docmodel.Node, error) {
			t := n.(*ast.Text)
			if source == nil {
				return docmodel.Node{}, fmt.Errorf("text importer requires markdown source buffer")
			}
			return docmodel.NewNode(KindText, Payload{
				Value: string(t.Segment.Value(source)),
			}, nil), nil
		},
	)
}
