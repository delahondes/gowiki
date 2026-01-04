package docmodel

import "reflect"

// Kind identifies the semantic nature of a node.
// Examples: "paragraph", "heading", "emph", "strike", "image".
type Kind string

const (
	KindDocument = Kind("document")
	KindFragment = Kind("fragment")
)

// Node is the concrete type for all nodes in the document tree.
type Node struct {
	Kind     Kind
	Payload  any
	Children []Node
}

func NewDocument(blocks ...Node) Node {
	return Node{
		Kind:     KindDocument,
		Payload:  struct{}{},
		Children: blocks,
	}
}

func NewFragment(blocks ...Node) Node {
	return Node{
		Kind:     KindFragment,
		Payload:  struct{}{},
		Children: blocks,
	}
}

func NewNode(kind Kind, payload any, children []Node) Node {
	return Node{
		Kind:     kind,
		Payload:  payload,
		Children: children,
	}
}

func IsZeroNode(n Node) bool {
	return n.Kind == "" && n.Payload == nil && len(n.Children) == 0
}

func (n Node) Equal(other Node) bool {
	if n.Kind != other.Kind {
		return false
	}
	if !reflect.DeepEqual(n.Payload, other.Payload) {
		return false
	}
	if len(n.Children) != len(other.Children) {
		return false
	}
	for i := range n.Children {
		if !n.Children[i].Equal(other.Children[i]) {
			return false
		}
	}
	return true
}
