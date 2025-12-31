package editorir

// Package editorir defines the canonical in-memory document model used by Gowiki.
//
// EditorIR is the authoritative representation of a document while it is being
// edited or transformed. It is independent from Markdown, HTML, and ProseMirror.
// Other representations are parsed into EditorIR or derived from it.

// Doc represents a full document.
// It is a linear sequence of top-level blocks.
type Doc struct {
	Blocks []Block
}

// ---- Blocks ----

// Block represents a block-level structural element of a document.
// Blocks form the top-level structure and may contain other blocks or inlines.
type Block interface{ isBlock() }

// Paragraph is a block containing inline content.
type Paragraph struct{ Inlines []Inline }

func (Paragraph) isBlock() {}

// Heading represents a section heading.
// Level follows Markdown semantics (1–6).
type Heading struct {
	Level   int
	Inlines []Inline
}

func (Heading) isBlock() {}

// BlockQuote represents a quoted block containing other blocks.
type BlockQuote struct{ Blocks []Block }

func (BlockQuote) isBlock() {}

// BulletList represents an unordered list.
type BulletList struct{ Items []ListItem }

func (BulletList) isBlock() {}

// OrderedList represents an ordered list.
// Start indicates the starting index of the list.
type OrderedList struct {
	Start int
	Items []ListItem
}

func (OrderedList) isBlock() {}

// ListItem represents a single list item containing block content.
type ListItem struct{ Blocks []Block }

// CodeBlock represents a fenced or indented code block.
// Text contains the raw code content without fencing.
type CodeBlock struct {
	Info string // fence info, optional
	Text string // raw code
}

func (CodeBlock) isBlock() {}

// HorizontalRule represents a thematic break.
type HorizontalRule struct{}

func (HorizontalRule) isBlock() {}

// ---- Inlines ----

// Inline represents an inline-level element.
// Inline nodes appear inside block content.
type Inline interface{ isInline() }

// Text represents raw textual content.
type Text struct{ Value string }

func (Text) isInline() {}

// HardBreak represents a forced line break within inline content.
type HardBreak struct{}

func (HardBreak) isInline() {}

// Emph represents emphasized (typically italic) inline content.
type Emph struct{ Inlines []Inline } // italic
func (Emph) isInline()               {}

// Strong represents strong emphasis (typically bold) inline content.
type Strong struct{ Inlines []Inline } // bold
func (Strong) isInline()               {}

// CodeSpan represents inline code content.
type CodeSpan struct{ Text string }

func (CodeSpan) isInline() {}

// Link represents a hyperlink with inline label content.
type Link struct {
	Href    string
	Title   string
	Inlines []Inline
}

func (Link) isInline() {}

// Image represents an inline image.
// Alt is plain text, not inline content.
type Image struct {
	Src   string
	Title string
	Alt   string
}

func (Image) isInline() {}
