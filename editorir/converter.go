package editorir

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func ParseMarkdownToIR(md string) (*Doc, error) {
	gm := goldmark.New()
	reader := text.NewReader([]byte(md))
	root := gm.Parser().Parse(reader)

	conv := &converter{
		src: []byte(md),
	}

	blocks, err := conv.convertBlockChildren(root)
	if err != nil {
		return nil, err
	}
	return &Doc{Blocks: blocks}, nil
}

type converter struct {
	src []byte
}

func (c *converter) convertBlockChildren(parent ast.Node) ([]Block, error) {
	var out []Block
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		b, err := c.convertBlock(n)
		if err != nil {
			return nil, err
		}
		if b != nil {
			out = append(out, b)
		}
	}
	return out, nil
}

func (c *converter) convertBlock(n ast.Node) (Block, error) {
	switch n.Kind() {

	case ast.KindParagraph:
		inl, err := c.convertInlineChildren(n)
		if err != nil {
			return nil, err
		}
		return Paragraph{Inlines: inl}, nil

	case ast.KindHeading:
		h := n.(*ast.Heading)
		inl, err := c.convertInlineChildren(n)
		if err != nil {
			return nil, err
		}
		return Heading{Level: h.Level, Inlines: inl}, nil

	case ast.KindBlockquote:
		blocks, err := c.convertBlockChildren(n)
		if err != nil {
			return nil, err
		}
		return BlockQuote{Blocks: blocks}, nil

	case ast.KindList:
		l := n.(*ast.List)
		items, err := c.convertListItems(n)
		if err != nil {
			return nil, err
		}
		if l.IsOrdered() {
			return OrderedList{Start: l.Start, Items: items}, nil
		}
		return BulletList{Items: items}, nil

	case ast.KindFencedCodeBlock:
		cb := n.(*ast.FencedCodeBlock)
		var buf bytes.Buffer
		for i := 0; i < cb.Lines().Len(); i++ {
			seg := cb.Lines().At(i)
			buf.Write(seg.Value(c.src))
		}
		return CodeBlock{
			Info: string(cb.Info.Text(c.src)),
			Text: buf.String(),
		}, nil

	case ast.KindCodeBlock:
		cb := n.(*ast.CodeBlock)
		var buf bytes.Buffer
		for i := 0; i < cb.Lines().Len(); i++ {
			seg := cb.Lines().At(i)
			buf.Write(seg.Value(c.src))
		}
		return CodeBlock{Text: buf.String()}, nil

	case ast.KindThematicBreak:
		return HorizontalRule{}, nil

	// You can ignore HTML blocks for now, or add a plugin hook later.
	case ast.KindHTMLBlock:
		return nil, nil

	default:
		return nil, fmt.Errorf("unsupported block node: %s", n.Kind().String())
	}
}

func (c *converter) convertListItems(list ast.Node) ([]ListItem, error) {
	var items []ListItem
	for it := list.FirstChild(); it != nil; it = it.NextSibling() {
		if it.Kind() != ast.KindListItem {
			continue
		}
		blocks, err := c.convertBlockChildren(it)
		if err != nil {
			return nil, err
		}
		items = append(items, ListItem{Blocks: blocks})
	}
	return items, nil
}

func (c *converter) convertInlineChildren(parent ast.Node) ([]Inline, error) {
	var out []Inline
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		inls, err := c.convertInline(n)
		if err != nil {
			return nil, err
		}
		out = append(out, inls...)
	}
	return out, nil
}

func (c *converter) convertInline(n ast.Node) ([]Inline, error) {
	switch n.Kind() {

	case ast.KindText:
		t := n.(*ast.Text)
		val := string(t.Segment.Value(c.src))

		var out []Inline
		if val != "" {
			out = append(out, Text{Value: val})
		}

		// Break semantics are encoded on Text nodes
		if t.HardLineBreak() {
			out = append(out, HardBreak{})
		} else if t.SoftLineBreak() {
			// POLICY: CommonMark softbreak behaves like a space.
			// If you prefer preserving a newline, use "\n" instead. -> do that to improve readability
			out = append(out, Text{Value: "\n"})
		}
		return out, nil

	case ast.KindEmphasis:
		e := n.(*ast.Emphasis)
		children, err := c.convertInlineChildren(n)
		if err != nil {
			return nil, err
		}
		// Goldmark uses Emphasis with Level: 1=em, 2=strong
		if e.Level == 2 {
			return []Inline{Strong{Inlines: children}}, nil
		}
		return []Inline{Emph{Inlines: children}}, nil

	case ast.KindCodeSpan:
		cs := n.(*ast.CodeSpan)
		return []Inline{CodeSpan{Text: string(cs.Text(c.src))}}, nil
	case ast.KindLink:
		l := n.(*ast.Link)
		children, err := c.convertInlineChildren(n)
		if err != nil {
			return nil, err
		}
		return []Inline{Link{
			Href:    string(l.Destination),
			Title:   string(l.Title),
			Inlines: children,
		}}, nil

	case ast.KindImage:
		img := n.(*ast.Image)
		// Goldmark keeps alt as child text nodes; simplest version:
		altInl, err := c.convertInlineChildren(n)
		if err != nil {
			return nil, err
		}
		alt := flattenText(altInl)
		return []Inline{Image{
			Src:   string(img.Destination),
			Title: string(img.Title),
			Alt:   alt,
		}}, nil

	default:
		return nil, fmt.Errorf("unsupported inline node: %s", n.Kind().String())
	}
}

func flattenText(inl []Inline) string {
	var buf bytes.Buffer
	for _, x := range inl {
		switch t := x.(type) {
		case Text:
			buf.WriteString(t.Value)
		}
	}
	return buf.String()
}
