package editorir

import (
	"bytes"
	"fmt"
)

// DebugString returns a simple string representation of the Doc for debugging.
func (d *Doc) DebugString() string {
	var buf bytes.Buffer
	for _, b := range d.Blocks {
		buf.WriteString(debugBlockString(b, 0))
	}
	return buf.String()
}

func debugBlockString(b Block, indent int) string {
	ind := func() string {
		return string(bytes.Repeat([]byte("  "), indent))
	}

	switch b := b.(type) {
	case Paragraph:
		return fmt.Sprintf("%sParagraph: %v\n", ind(), b.Inlines)
	case Heading:
		return fmt.Sprintf("%sHeading (Level %d): %v\n", ind(), b.Level, b.Inlines)
	case BlockQuote:
		var buf bytes.Buffer
		buf.WriteString(fmt.Sprintf("%sBlockQuote:\n", ind()))
		for _, bb := range b.Blocks {
			buf.WriteString(debugBlockString(bb, indent+1))
		}
		return buf.String()
	case BulletList:
		var buf bytes.Buffer
		buf.WriteString(fmt.Sprintf("%sBulletList:\n", ind()))
		for _, item := range b.Items {
			buf.WriteString(fmt.Sprintf("%s- ListItem:\n", ind()))
			for _, bb := range item.Blocks {
				buf.WriteString(debugBlockString(bb, indent+2))
			}
		}
		return buf.String()
	case OrderedList:
		var buf bytes.Buffer
		buf.WriteString(fmt.Sprintf("%sOrderedList (Start %d):\n", ind(), b.Start))
		for i, item := range b.Items {
			buf.WriteString(fmt.Sprintf("%s%d. ListItem:\n", ind(), i+b.Start))
			for _, bb := range item.Blocks {
				buf.WriteString(debugBlockString(bb, indent+2))
			}
		}
		return buf.String()
	case CodeBlock:
		return fmt.Sprintf("%sCodeBlock (Info: %q): %q\n", ind(), b.Info, b.Text)
	case HorizontalRule:
		return fmt.Sprintf("%sHorizontalRule\n", ind())
	default:
		return fmt.Sprintf("%sUnknown Block Type\n", ind())
	}
}
