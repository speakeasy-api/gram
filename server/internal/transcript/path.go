package transcript

import "slices"

// Path returns the trajectory that ends at leaf: the entries from its tree
// root to leaf, in order. It stops at a root. After a compaction the root is
// the summary, which is what the model sees first. It also stops before a
// parent that was not loaded.
func Path(leaf *Node) []*Node {
	var out []*Node
	for n := leaf; visible(n); n = n.Parent {
		out = append(out, n)
	}
	slices.Reverse(out)
	return out
}

// FullPath is Path, but at each compaction root it continues from the old
// leaf, so it covers the whole session up to leaf.
func FullPath(leaf *Node) []*Node {
	var out []*Node
	for n := leaf; visible(n); {
		out = append(out, n)
		switch {
		case n.Parent != nil && !n.Parent.hidden:
			n = n.Parent
		case n.Continues != nil:
			n = n.Continues
		default:
			n = nil
		}
	}
	slices.Reverse(out)
	return out
}

func visible(n *Node) bool {
	return n != nil && !n.hidden && !n.placeholder
}
