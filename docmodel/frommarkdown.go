package docmodel

import (
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ImportGoldmark converts a Goldmark AST root into a docmodel Node.
func ImportGoldmark(root ast.Node, source []byte) (Node, error) {
	var out []Node
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		dn, err := importNode(n, source)
		if err != nil {
			return Node{}, err
		}
		if !IsZeroNode(dn) {
			out = append(out, dn)
		}
	}
	if len(out) == 0 {
		return Node{}, nil
	}
	if len(out) == 1 {
		return out[0], nil
	}
	return NewDocument(out...), nil
}

func importNode(n ast.Node, source []byte) (Node, error) {
	// 1. If a plugin registered an importer, use it
	if fn, ok := goldmarkImporters[n.Kind()]; ok {
		node, err := fn(
			n,
			source,
			func(node ast.Node, src []byte) ([]Node, error) {
				return importChildren(node, src), nil
			},
		)
		if err != nil {
			return Node{}, err
		}
		// Validate and normalize with NodeSpec if available
		return NormalizeNode(node)
	}

	// 2. Default behavior: recurse into children without flattening
	children := importChildren(n, source)
	if len(children) > 0 {
		// Otherwise, fallback to fragment
		return NewFragment(children...), nil
	}

	return Node{}, fmt.Errorf(
		"goldmark node %T (kind=%v) has no importer and no children",
		n, n.Kind(),
	)
}

func importChildren(n ast.Node, source []byte) []Node {
	var out []Node
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		dn, err := importNode(c, source)
		if err != nil {
			panic(fmt.Errorf("import error: %w", err))
		}
		if !IsZeroNode(dn) {
			out = append(out, dn)
		}
	}
	return out
}

// ParseMarkdown parses raw Markdown input into a docmodel Node.
func ParseMarkdown(input []byte) (Node, error) {
	md := goldmark.New()
	reader := text.NewReader(input)
	root := md.Parser().Parse(reader)
	return ImportGoldmark(root, input)
}
