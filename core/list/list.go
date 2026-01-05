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

func renderChildrenHTML(children []docmodel.Node) (string, error) {
	var b strings.Builder
	for _, blk := range children {
		s, err := docmodel.RenderHTML(blk)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

func indentContinuation(s, indent string) string {
	// Prefix every line after the first with indent (for multi-line content in a list item)
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return s
	}
	var b strings.Builder
	b.WriteString(lines[0])
	for i := 1; i < len(lines); i++ {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString(lines[i])
	}
	return b.String()
}

func renderListMarkdown(list docmodel.Node, indent string) (string, error) {
	// list.Kind must be KindList, but we won't re-validate here (fail fast elsewhere)
	var b strings.Builder
	itemPrefix := indent + "- "
	contIndent := indent + "  "

	for _, it := range list.Children {
		b.WriteString(itemPrefix)

		firstBlock := true
		for _, blk := range it.Children {
			if blk.Kind == KindList {
				// Nested list: start it on a new line, indented under the item
				if !firstBlock {
					b.WriteString("\n")
					b.WriteString(contIndent)
				} else {
					b.WriteString("\n")
					b.WriteString(contIndent)
				}
				nested, err := renderListMarkdown(blk, contIndent)
				if err != nil {
					return "", err
				}
				// nested already includes its own newlines/indentation
				b.WriteString(nested)
				firstBlock = false
				continue
			}

			s, err := docmodel.EmitMarkdown(blk)
			if err != nil {
				return "", err
			}
			if s == "" {
				continue
			}
			if !firstBlock {
				b.WriteString("\n")
				b.WriteString(contIndent)
			}
			b.WriteString(indentContinuation(s, contIndent))
			firstBlock = false
		}

		b.WriteString("\n")
	}

	// Blank line after list, consistent with common markdown formatting
	b.WriteString("\n")
	return b.String(), nil
}

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
			return renderListMarkdown(n, "")
		},
	)

	docmodel.RegisterMarkdown(
		KindListItem,
		func() any { return struct{}{} },
		nil,
		func(n docmodel.Node) (string, error) {
			var b strings.Builder
			for _, blk := range n.Children {
				s, _ := docmodel.EmitMarkdown(blk)
				b.WriteString(s)
			}
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
				s, err := renderChildrenHTML(it.Children)
				if err != nil {
					return "", err
				}
				b.WriteString(s)
				b.WriteString("</li>")
			}
			b.WriteString("</ul>")
			return b.String(), nil
		},
	)

	docmodel.RegisterHTML(
		KindListItem,
		func(n docmodel.Node) (string, error) {
			var b strings.Builder
			b.WriteString("<li>")
			s, err := renderChildrenHTML(n.Children)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			b.WriteString("</li>")
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

	docmodel.RegisterDebug(
		KindListItem,
		func(n docmodel.Node, indent int) string {
			prefix := strings.Repeat("  ", indent)
			var b strings.Builder
			b.WriteString(prefix + "LIST_ITEM\n")
			for _, blk := range n.Children {
				b.WriteString(docmodel.Debug(blk, indent+1))
			}
			return b.String()
		},
	)
}
