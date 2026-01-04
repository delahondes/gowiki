package list

import (
	"strconv"
	"strings"

	"github.com/delahondes/gowiki/docmodel"
	"github.com/yuin/goldmark/ast"
)

const (
	KindList     docmodel.Kind = "bullet_list"
	KindListItem docmodel.Kind = "bullet_list_item"
)

func init() {
	// Goldmark -> docmodel
	docmodel.RegisterGoldmarkImporter(ast.KindList,
		func(
			n ast.Node,
			source []byte,
			importChildren func(ast.Node, []byte) ([]docmodel.Node, error),
		) (docmodel.Node, error) {
			listNode := n.(*ast.List)

			var items []docmodel.Node
			for it := listNode.FirstChild(); it != nil; it = it.NextSibling() {
				itemNode := it.(*ast.ListItem)

				var blocks []docmodel.Node
				for c := itemNode.FirstChild(); c != nil; c = c.NextSibling() {
					children, err := importChildren(c, source)
					if err != nil {
						return docmodel.Node{}, err
					}
					blocks = append(blocks, children...)
				}

				items = append(items, docmodel.NewNode(
					KindListItem,
					struct{}{},
					blocks,
				))
			}

			return docmodel.NewNode(KindList, struct{}{}, items), nil
		},
	)

	docmodel.RegisterMarkdown(
		KindList,
		func() any { return struct{}{} },
		nil,
		func(n docmodel.Node) (string, error) {
			var b strings.Builder
			for _, it := range n.Children {
				b.WriteString("- ")
				for _, blk := range it.Children {
					s, _ := docmodel.EmitMarkdown(blk)
					b.WriteString(s)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
			return b.String(), nil
		},
	)

	docmodel.RegisterHTML(
		KindList,
		func(n docmodel.Node) (string, error) {
			var b strings.Builder
			b.WriteString("<ul>")
			for _, it := range n.Children {
				b.WriteString("<li>")
				for _, blk := range it.Children {
					s, _ := docmodel.RenderHTML(blk)
					b.WriteString(s)
				}
				b.WriteString("</li>")
			}
			b.WriteString("</ul>")
			return b.String(), nil
		},
	)

	docmodel.RegisterDebug(
		KindList,
		func(n docmodel.Node, indent int) string {
			prefix := strings.Repeat("  ", indent)
			var b strings.Builder
			b.WriteString(prefix + "BULLET_LIST\n")
			for i, it := range n.Children {
				b.WriteString(prefix + "  ITEM " + strconv.Itoa(i) + "\n")
				for _, blk := range it.Children {
					b.WriteString(docmodel.Debug(blk, indent+2))
				}
			}
			return b.String()
		},
	)
}
