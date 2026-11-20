package transcript

import (
	"errors"
	"fmt"
	"strings"
)

// copySep joins a natural ID to the tree a partial compaction copied it into.
// Merge rejects a delivered ID that contains it, so a scoped ID cannot
// collide with a real one, and the text before it is always the original.
const copySep = "#copy-in:"

// ErrReservedID reports a delivered natural ID that contains the separator
// the merge keeps for partial-compaction copies.
var ErrReservedID = errors.New("transcript: natural id contains the reserved copy separator")

// checkIDs rejects a delivery with a natural ID that contains copySep.
func checkIDs(d Delivery) error {
	check := func(id *NaturalID) error {
		if id != nil && strings.Contains(id.Value, copySep) {
			return fmt.Errorf("%w: kind %s", ErrReservedID, id.Kind)
		}
		return nil
	}
	for _, l := range d.Links {
		if err := errors.Join(check(&l.ID), check(l.To)); err != nil {
			return err
		}
	}
	for _, e := range d.Entries {
		for _, id := range e.IDs {
			if err := check(&id); err != nil {
				return err
			}
		}
		if err := errors.Join(check(e.Parent), check(e.Continues)); err != nil {
			return err
		}
	}
	return nil
}

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

// scopeAll scopes every ID to the tree rooted at root.
func scopeAll(ids []NaturalID, root *Node) []NaturalID {
	out := make([]NaturalID, 0, len(ids))
	for _, id := range ids {
		out = append(out, scoped(id, root))
	}
	return out
}

// unscoped returns the original ID of a scoped one.
func unscoped(id NaturalID) (NaturalID, bool) {
	i := strings.LastIndex(id.Value, copySep)
	if i < 0 {
		return id, false
	}
	return NaturalID{Kind: id.Kind, Value: id.Value[:i], Block: id.Block}, true
}
