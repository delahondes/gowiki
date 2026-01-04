package docmodel

import (
	"reflect"

	"github.com/yuin/goldmark/ast"
)

type (
	MarkdownParser  func(input string) ([]Node, error)
	MarkdownEmitter func(Node) (string, error)
	HTMLRenderer    func(Node) (string, error)
	DebugRenderer   func(Node, int) string

	// Goldmark AST → docmodel importers
	GoldmarkImporter func(
		n ast.Node,
		source []byte,
		importChildren func(ast.Node, []byte) ([]Node, error),
	) (Node, error)

	PayloadType func() any
)

var (
	mdParsers    = map[Kind]MarkdownParser{}
	mdEmitters   = map[Kind]MarkdownEmitter{}
	htmlRenders  = map[Kind]HTMLRenderer{}
	debugRenders = map[Kind]DebugRenderer{}

	goldmarkImporters = map[ast.NodeKind]GoldmarkImporter{}
)

func RegisterMarkdown(kind Kind, payload PayloadType, p MarkdownParser, e MarkdownEmitter) {
	val := payload()
	t := reflect.TypeOf(val)
	if !t.Comparable() {
		panic("payload type for kind " + string(kind) + " is not comparable")
	}
	mdParsers[kind] = p
	mdEmitters[kind] = e
}

func RegisterHTML(kind Kind, r HTMLRenderer) {
	htmlRenders[kind] = r
}

func RegisterDebug(kind Kind, r DebugRenderer) {
	debugRenders[kind] = r
}

func RegisterGoldmarkImporter(kind ast.NodeKind, fn GoldmarkImporter) {
	goldmarkImporters[kind] = fn
}
