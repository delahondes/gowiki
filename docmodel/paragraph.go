package docmodel

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
)

const KindParagraph Kind = "paragraph"

func CoerceParagraphChildren(_ Node, children []Node) ([]Node, error) {
	for _, c := range children {
		spec, ok := GetNodeSpec(c.Kind)
		if !ok {
			return nil, fmt.Errorf("no NodeSpec for child kind %s", c.Kind)
		}
		if spec.Flow != FlowInline {
			return nil, fmt.Errorf(
				"paragraph cannot contain block child %s",
				c.Kind,
			)
		}
	}
	return children, nil
}

func init() {

	RegisterNodeSpec(NodeSpec{
		Kind:           KindParagraph,
		Flow:           FlowBlock,
		ChildrenFlow:   Ptr(FlowInline),
		CoerceChildren: CoerceParagraphChildren,
	})

	// Goldmark -> docmodel
	RegisterGoldmarkImporter(ast.KindParagraph,
		func(
			n ast.Node,
			source []byte,
			importChildren func(ast.Node, []byte) ([]Node, error),
		) (Node, error) {
			children, err := importChildren(n, source)
			if err != nil {
				return Node{}, err
			}
			return NewNode(KindParagraph, struct{}{}, children), nil
		},
	)

	RegisterMarkdown(
		KindParagraph,
		func() any { return struct{}{} },
		nil,
		func(n Node) (string, error) {
			var b strings.Builder
			for _, c := range n.Children {
				s, _ := EmitMarkdown(c)
				b.WriteString(s)
			}
			return b.String() + "\n\n", nil
		},
	)
	RegisterHTML(KindParagraph, func(n Node) (string, error) {
		var b strings.Builder
		b.WriteString("<p>")
		for _, c := range n.Children {
			s, _ := RenderHTML(c)
			b.WriteString(s)
		}
		b.WriteString("</p>")
		return b.String(), nil
	})
	RegisterDebug(KindParagraph, func(n Node, indent int) string {
		prefix := strings.Repeat("  ", indent)
		out := fmt.Sprintf("%sPARAGRAPH\n", prefix)
		for _, c := range n.Children {
			out += Debug(c, indent+1)
		}
		return out
	})

}

func NewParagraph(children ...Node) Node {
	return NewNode(KindParagraph, struct{}{}, children)
}

const KindPseudoParagraph Kind = "pseudo_paragraph"

func init() {

	RegisterNodeSpec(NodeSpec{
		Kind:         KindPseudoParagraph,
		Flow:         FlowBlock,
		ChildrenFlow: Ptr(FlowInline),
	})

	RegisterMarkdown(
		KindPseudoParagraph,
		func() any { return struct{}{} },
		nil,
		func(n Node) (string, error) {
			var b strings.Builder
			for _, c := range n.Children {
				s, _ := EmitMarkdown(c)
				b.WriteString(s)
			}
			return b.String(), nil
		},
	)
	RegisterHTML(KindPseudoParagraph, func(n Node) (string, error) {
		var b strings.Builder
		b.WriteString("<p>")
		for _, c := range n.Children {
			s, _ := RenderHTML(c)
			b.WriteString(s)
		}
		b.WriteString("</p>")
		return b.String(), nil
	})
	RegisterDebug(KindPseudoParagraph, func(n Node, indent int) string {
		prefix := strings.Repeat("  ", indent)
		out := fmt.Sprintf("%sPSEUDO PARAGRAPH\n", prefix)
		for _, c := range n.Children {
			out += Debug(c, indent+1)
		}
		return out
	})

}

func NewPseudoParagraph(children ...Node) Node {
	return NewNode(KindPseudoParagraph, struct{}{}, children)
}
