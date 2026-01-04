package test

import (
	"testing"

	"github.com/delahondes/gowiki/core/emph"
	"github.com/delahondes/gowiki/core/list"

	_ "github.com/delahondes/gowiki/core"

	"github.com/delahondes/gowiki/docmodel"
)

func TestDocModelCorePlugins(t *testing.T) {
	input := `
Hello *world* and *universe*

- one
- *two*
`

	doc, err := docmodel.ParseMarkdown([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if docmodel.IsZeroNode(doc) {
		t.Fatal("doc is nil")
	}

	if len(doc.Children) == 0 {
		t.Fatal("expected at least one block")
	}

	t.Log("==== DEBUG TREE ====")
	t.Log(docmodel.Debug(doc, 0))

	// Very light structural assertions (intentionally minimal)
	hasEmph := false
	hasList := false

	var walk func(n docmodel.Node)
	walk = func(n docmodel.Node) {
		switch n.Kind {
		case emph.KindEmph:
			hasEmph = true
		case list.KindList:
			hasList = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}

	walk(doc)

	if !hasEmph {
		t.Error("expected an emphasis block")
	}
	if !hasList {
		t.Error("expected a bullet list block")
	}

	// Round-trip integration test: md → dm → md → dm.
	// This validates plugin completeness by ensuring the document can be emitted to
	// Markdown and parsed again successfully (no strict string equality enforced).
	md, err := docmodel.EmitMarkdown(doc)
	if err != nil {
		t.Fatalf("EmitMarkdown error: %v", err)
	}
	t.Log("==== EMITTED MARKDOWN ====")
	t.Log(string(md))
	doc2, err := docmodel.ParseMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("parse error on round-tripped Markdown: %v", err)
	}
	if docmodel.IsZeroNode(doc2) {
		t.Fatal("doc is nil after round-tripping Markdown")
	}

	if !doc.Equal(doc2) {
		t.Errorf("round-tripped document is not equal to original\nOriginal:\n%s\nRound-tripped:\n%s",
			docmodel.Debug(doc, 0), docmodel.Debug(doc2, 0))
	}
}
