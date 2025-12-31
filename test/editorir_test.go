package test

import (
	"testing"

	"github.com/delahondes/gowiki/editorir"
)

func TestParseMarkdownToIR(t *testing.T) {
	doc, err := editorir.ParseMarkdownToIR("Hello *world*\n\n# Title\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) == 0 {
		t.Fatal("expected blocks")
	}
	t.Logf("Parsed Doc:\n%s", doc.DebugString())
}
