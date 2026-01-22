package docmodel

import "fmt"

type Flow int

const (
	FlowInline Flow = iota
	FlowBlock
)

func Ptr[T any](v T) *T {
	return &v
}

type NodeSpec struct {
	Kind            Kind
	Flow            Flow
	ChildrenFlow    *Flow                                              // nil = no children
	AllowedChildren []Kind                                             // nil = flow-based
	CoerceChildren  func(parent Node, children []Node) ([]Node, error) // optional
}

var nodeSpecs = map[Kind]NodeSpec{}

func RegisterNodeSpec(spec NodeSpec) {
	if _, exists := nodeSpecs[spec.Kind]; exists {
		panic("duplicate NodeSpec for kind " + string(spec.Kind))
	}
	nodeSpecs[spec.Kind] = spec
}

func GetNodeSpec(kind Kind) (NodeSpec, bool) {
	spec, ok := nodeSpecs[kind]
	return spec, ok
}

func AllNodeSpecs() map[Kind]NodeSpec {
	return nodeSpecs
}

func NormalizeNode(n Node) (Node, error) {
	spec, ok := GetNodeSpec(n.Kind)
	if !ok {
		// Unknown docmodel node: leave untouched
		return n, nil
	}

	var normalizedChildren []Node
	for _, c := range n.Children {
		nc, err := NormalizeNode(c)
		if err != nil {
			return Node{}, err
		}
		normalizedChildren = append(normalizedChildren, nc)
	}
	n.Children = normalizedChildren

	if spec.CoerceChildren != nil {
		var err error
		n.Children, err = spec.CoerceChildren(n, n.Children)
		if err != nil {
			return Node{}, err
		}
	}

	if spec.ChildrenFlow == nil && spec.AllowedChildren == nil {
		// No children allowed
		if len(n.Children) > 0 {
			return Node{}, fmt.Errorf(
				"node %s must not have children",
				n.Kind,
			)
		}
		return n, nil
	}

	var out []Node
	for _, c := range n.Children {
		childSpec, ok := GetNodeSpec(c.Kind)
		if !ok {
			return Node{}, fmt.Errorf(
				"no NodeSpec for child kind %s",
				c.Kind,
			)
		}

		allowed := false

		if spec.AllowedChildren != nil {
			for _, k := range spec.AllowedChildren {
				if c.Kind == k {
					allowed = true
					break
				}
			}
		} else if spec.ChildrenFlow != nil {
			if childSpec.Flow == *spec.ChildrenFlow {
				allowed = true
			}
		}

		if !allowed {
			return Node{}, fmt.Errorf(
				"child %s not allowed in %s",
				c.Kind,
				n.Kind,
			)
		}

		out = append(out, c)
	}

	n.Children = out
	return n, nil
}

func WrapInlines(
	children []Node,
	isInline func(Node) bool,
	wrap func([]Node) Node,
) ([]Node, error) {
	var out []Node
	var buf []Node

	flush := func() {
		if len(buf) == 0 {
			return
		}
		out = append(out, wrap(buf))
		buf = nil
	}

	for _, c := range children {
		if isInline(c) {
			buf = append(buf, c)
		} else {
			flush()
			out = append(out, c)
		}
	}

	flush()
	return out, nil
}
