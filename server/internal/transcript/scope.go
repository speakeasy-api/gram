package transcript

import "strings"

// copySep joins a natural ID to the tree a partial compaction copied it into.
// A model or client never makes an ID with it, so a scoped ID cannot collide
// with a real one in the database.
const copySep = "#copy-in:"

// scoped returns the ID a copy of an entry carries inside the tree rooted at
// root. Claude Code repeats kept messages after a summary with their original
// IDs; the copies are new entries of the new tree, so their IDs must not
// match the originals.
func scoped(id NaturalID, root *Node) NaturalID {
	if root == nil {
		return id
	}
	return NaturalID{Kind: id.Kind, Value: id.Value + copySep + root.ID.String(), Block: id.Block}
}

// unscoped returns the original ID of a scoped one.
func unscoped(id NaturalID) (NaturalID, bool) {
	i := strings.LastIndex(id.Value, copySep)
	if i < 0 {
		return id, false
	}
	return NaturalID{Kind: id.Kind, Value: id.Value[:i], Block: id.Block}, true
}
