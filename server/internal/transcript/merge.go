package transcript

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// Node is one stored entry in the tree.
type Node struct {
	ID uuid.UUID
	// Parent is the entry before this one. For a tree root it is the
	// transcript's hidden root; ParentID then reports no parent.
	Parent *Node
	// Continues is the old leaf a compaction root continues from.
	Continues *Node
	// Children are kept in the order they were added, oldest first.
	Children []*Node
	// Entry holds what the node says. A node loaded from storage carries only
	// its role and natural IDs; its text stays in the database.
	Entry      Entry
	ContentKey []byte
	PathKey    []byte

	hidden bool
}

// ParentID is the parent to store in chat_messages.parent_message_id.
func (n *Node) ParentID() uuid.NullUUID {
	if n.Parent == nil || n.Parent.hidden {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: n.Parent.ID, Valid: true}
}

// ContinuesID is the old leaf to store in chat_messages.continues_message_id.
func (n *Node) ContinuesID() uuid.NullUUID {
	if n.Continues == nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: n.Continues.ID, Valid: true}
}

// Stored is a node as the chat writer loads it from the database.
type Stored struct {
	ID          uuid.UUID
	ParentID    uuid.NullUUID
	ContinuesID uuid.NullUUID
	Role        Role
	Synthetic   bool
	ContentKey  []byte
	PathKey     []byte
	IDs         []NaturalID
}

// Seen is a delivered entry that was already stored.
type Seen struct {
	// Index is the entry's position in Delivery.Entries.
	Index int
	Node  *Node
	// NewIDs are natural IDs the entry brought that the node did not have,
	// for example a transcript line that matched an inference entry. The
	// writer stores them; the node now has IDs, so its sibling_key clears.
	NewIDs []NaturalID
}

// Alias maps the ID of a dropped source line to the entry that stands for it.
type Alias struct {
	ID   NaturalID
	Node *Node
}

// Result is what a Merge changed.
type Result struct {
	// Added are the new nodes, in the order they were placed. Only these go
	// to risk scanning, metering and events.
	Added []*Node
	Seen  []Seen
	// Aliases are links the writer stores so later deliveries can name them.
	Aliases []Alias
}

// UnknownParentError reports a delivery that names a parent the transcript
// does not have.
type UnknownParentError struct {
	ID NaturalID
}

func (e *UnknownParentError) Error() string {
	return fmt.Sprintf("transcript: unknown parent %s:%s/%d", e.ID.Kind, e.ID.Value, e.ID.Block)
}

type idKey struct {
	kind  Kind
	value string
}

// Transcript is the tree of one session. It is not safe for concurrent use;
// the writer serializes deliveries for one chat.
type Transcript struct {
	root      *Node
	byID      map[NaturalID]*Node
	lastBlock map[idKey]*Node
	aliases   map[NaturalID]*Node
	newID     func() (uuid.UUID, error)
}

// New returns an empty transcript. newID makes the ID of every added node.
func New(newID func() (uuid.UUID, error)) *Transcript {
	return &Transcript{
		root: &Node{
			ID:         uuid.Nil,
			Parent:     nil,
			Continues:  nil,
			Children:   nil,
			Entry:      Entry{IDs: nil, Parent: nil, Continues: nil, Synthetic: false, Role: "", Text: "", Call: nil, Result: nil, MessageID: "", At: zeroTime},
			ContentKey: nil,
			PathKey:    nil,
			hidden:     true,
		},
		byID:      map[NaturalID]*Node{},
		lastBlock: map[idKey]*Node{},
		aliases:   map[NaturalID]*Node{},
		newID:     newID,
	}
}

// Load adds stored nodes, parents before children. A node whose parent was
// not loaded hangs under a placeholder with that ID, so a partial load still
// answers lookups among the children of the loaded nodes.
func (t *Transcript) Load(stored []Stored) {
	nodes := make(map[uuid.UUID]*Node, len(stored))
	get := func(id uuid.NullUUID) *Node {
		if !id.Valid {
			return nil
		}
		if n, ok := nodes[id.UUID]; ok {
			return n
		}
		n := t.blank(id.UUID)
		nodes[id.UUID] = n
		return n
	}
	for _, s := range stored {
		n, ok := nodes[s.ID]
		if !ok {
			n = t.blank(s.ID)
			nodes[s.ID] = n
		}
		n.Entry.Role = s.Role
		n.Entry.Synthetic = s.Synthetic
		n.Entry.IDs = append(n.Entry.IDs, s.IDs...)
		n.ContentKey = s.ContentKey
		n.PathKey = s.PathKey
		n.Continues = get(s.ContinuesID)
		parent := get(s.ParentID)
		if parent == nil {
			parent = t.root
		}
		n.Parent = parent
		parent.Children = append(parent.Children, n)
		t.register(n, s.IDs)
	}
}

// Merge places every entry of d in the tree and returns what changed.
// Merging the same delivery twice adds nothing the second time. On error the
// tree may hold part of the delivery, so the caller must discard it.
//
// A compaction link is resolved only inside the delivery that carries it:
// the shipper must send a compaction boundary and the summary after it in
// one delivery.
func (t *Transcript) Merge(d Delivery) (Result, error) {
	res := Result{Added: nil, Seen: nil, Aliases: nil}
	links := make(map[NaturalID]Link, len(d.Links))
	for _, l := range d.Links {
		links[l.ID] = l
	}

	cursor := t.root
	for i, e := range d.Entries {
		parent, continues, explicit, err := t.start(cursor, e, links)
		if err != nil {
			return Result{Added: nil, Seen: nil, Aliases: nil}, err
		}

		if n := t.find(parent, e); n != nil {
			res.Seen = append(res.Seen, Seen{Index: i, Node: n, NewIDs: t.adopt(n, e.IDs)})
			cursor = n
			continue
		}

		at := parent
		if explicit && continues == nil {
			at = t.placement(parent, e)
		}
		n, err := t.attach(at, continues, e)
		if err != nil {
			return Result{Added: nil, Seen: nil, Aliases: nil}, err
		}
		res.Added = append(res.Added, n)
		cursor = n
	}

	for _, l := range d.Links {
		if l.Compaction || l.To == nil {
			continue
		}
		if n, ok := t.lookup(*l.To); ok {
			t.aliases[l.ID] = n
			res.Aliases = append(res.Aliases, Alias{ID: l.ID, Node: n})
		}
	}
	return res, nil
}

// start finds where an entry goes: under the previous entry, under the
// parent it names, or at a new root after a compaction. explicit reports
// that the entry named its parent.
func (t *Transcript) start(cursor *Node, e Entry, links map[NaturalID]Link) (parent, continues *Node, explicit bool, err error) {
	switch {
	case e.Continues != nil:
		old, ok := t.lookup(*e.Continues)
		if !ok {
			return nil, nil, false, &UnknownParentError{ID: *e.Continues}
		}
		return t.root, old, true, nil
	case e.Parent != nil:
		parent, continues, err := t.resolve(*e.Parent, links, 0)
		return parent, continues, true, err
	default:
		return cursor, nil, false, nil
	}
}

// resolve turns a parent reference into a node, following links for lines
// that have no entry. A compaction link makes the entry a new tree root.
func (t *Transcript) resolve(ref NaturalID, links map[NaturalID]Link, depth int) (parent, continues *Node, err error) {
	if n, ok := t.lookup(ref); ok {
		return n, nil, nil
	}
	l, ok := links[ref]
	if !ok && ref.Block == LastBlock {
		whole := ref
		whole.Block = 0
		l, ok = links[whole]
	}
	if !ok || depth > len(links) {
		return nil, nil, &UnknownParentError{ID: ref}
	}
	if l.To == nil {
		return t.root, nil, nil
	}
	target, _, err := t.resolve(*l.To, links, depth+1)
	if err != nil {
		return nil, nil, err
	}
	if l.Compaction {
		return t.root, target, nil
	}
	return target, nil, nil
}

// find returns the stored node that e already is, or nil.
//
// A natural ID match anywhere in the transcript is certain. Otherwise the
// content key is matched among the children of parent, newest first, so an
// entry without IDs joins the branch in use now. A child does not match when
// it and e both have an ID of the same kind: two prompt IDs, or two
// transcript lines, are two entries even with the same text (a rewind).
func (t *Transcript) find(parent *Node, e Entry) *Node {
	for _, id := range e.IDs {
		if n, ok := t.byID[id]; ok {
			return n
		}
	}
	key := ContentKey(e)
	for _, c := range slices.Backward(parent.Children) {
		if bytes.Equal(c.ContentKey, key) && !sameKind(c.Entry.IDs, e.IDs) {
			return c
		}
	}
	return nil
}

// placement keeps the side entries of one step on one line. When the parent
// a line names already has children, a new prompt is a rewind and branches
// there; anything else (a tool result, an attachment's follower, a parallel
// tool call) continues the newest line under that parent.
func (t *Transcript) placement(parent *Node, e Entry) *Node {
	if parent.hidden || isTurn(e) {
		return parent
	}
	n := parent
	for len(n.Children) > 0 {
		n = n.Children[len(n.Children)-1]
	}
	return n
}

func (t *Transcript) attach(parent, continues *Node, e Entry) (*Node, error) {
	id, err := t.newID()
	if err != nil {
		return nil, fmt.Errorf("transcript: new node id: %w", err)
	}
	key := ContentKey(e)
	n := &Node{
		ID:         id,
		Parent:     parent,
		Continues:  continues,
		Children:   nil,
		Entry:      e,
		ContentKey: key,
		PathKey:    PathKey(parent.PathKey, key),
		hidden:     false,
	}
	parent.Children = append(parent.Children, n)
	t.register(n, e.IDs)
	return n, nil
}

// adopt gives n the IDs it did not have yet and returns them.
func (t *Transcript) adopt(n *Node, ids []NaturalID) []NaturalID {
	var added []NaturalID
	for _, id := range ids {
		if _, ok := t.byID[id]; ok {
			continue
		}
		added = append(added, id)
	}
	n.Entry.IDs = append(n.Entry.IDs, added...)
	t.register(n, added)
	return added
}

func (t *Transcript) register(n *Node, ids []NaturalID) {
	for _, id := range ids {
		t.byID[id] = n
		k := idKey{kind: id.Kind, value: id.Value}
		if prev, ok := t.lastBlock[k]; !ok || id.Block >= blockOf(prev, k) {
			t.lastBlock[k] = n
		}
	}
}

func (t *Transcript) lookup(ref NaturalID) (*Node, bool) {
	if ref.Block == LastBlock {
		n, ok := t.lastBlock[idKey{kind: ref.Kind, value: ref.Value}]
		if ok {
			return n, true
		}
		ref.Block = 0
	}
	if n, ok := t.byID[ref]; ok {
		return n, true
	}
	n, ok := t.aliases[ref]
	return n, ok
}

func (t *Transcript) blank(id uuid.UUID) *Node {
	return &Node{
		ID:         id,
		Parent:     nil,
		Continues:  nil,
		Children:   nil,
		Entry:      Entry{IDs: nil, Parent: nil, Continues: nil, Synthetic: false, Role: "", Text: "", Call: nil, Result: nil, MessageID: "", At: zeroTime},
		ContentKey: nil,
		PathKey:    nil,
		hidden:     false,
	}
}

func blockOf(n *Node, k idKey) int {
	block := -1
	for _, id := range n.Entry.IDs {
		if id.Kind == k.kind && id.Value == k.value && id.Block > block {
			block = id.Block
		}
	}
	return block
}

func sameKind(a, b []NaturalID) bool {
	for _, x := range a {
		for _, y := range b {
			if x.Kind == y.Kind {
				return true
			}
		}
	}
	return false
}
