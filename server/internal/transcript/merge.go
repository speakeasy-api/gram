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
	// placeholder marks a node known only by its ID: a parent or old leaf
	// that a partial load did not include. Paths stop before it.
	placeholder bool
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
	// Continues is set when the entry linked a stored root to the old leaf it
	// continues from, for example a transcript summary that matched a root an
	// inference frame stored first. The writer stores it.
	Continues *Node
}

// Alias maps the ID of a dropped source line to the entry that stands for it.
type Alias struct {
	ID NaturalID
	// Node is the entry the line stands for. Nil means the line stands for
	// the start of the conversation.
	Node *Node
}

// Result is what a Merge changed.
type Result struct {
	// Added are the new nodes, in the order they were placed. Only these go
	// to risk scanning, metering and events.
	Added []*Node
	Seen  []Seen
	// Aliases are links that are new or changed, for the writer to store so
	// later deliveries can name them.
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
	nodes     map[uuid.UUID]*Node
	byID      map[NaturalID]*Node
	lastBlock map[idKey]*Node
	aliases   map[NaturalID]*Node
	newID     func() (uuid.UUID, error)
}

// New returns an empty transcript. newID makes the ID of every added node.
func New(newID func() (uuid.UUID, error)) *Transcript {
	root := blankNode(uuid.Nil)
	root.hidden = true
	return &Transcript{
		root:      root,
		nodes:     map[uuid.UUID]*Node{},
		byID:      map[NaturalID]*Node{},
		lastBlock: map[idKey]*Node{},
		aliases:   map[NaturalID]*Node{},
		newID:     newID,
	}
}

// Load adds stored nodes, parents before children. It can be called more
// than once: a node whose parent is not loaded yet hangs under a placeholder
// with that ID, and a later Load of that parent fills the same placeholder,
// so incremental loads stay connected.
func (t *Transcript) Load(stored []Stored) {
	for _, s := range stored {
		n := t.node(s.ID)
		if !n.placeholder {
			continue // loaded before
		}
		n.placeholder = false
		n.Entry.Role = s.Role
		n.Entry.Synthetic = s.Synthetic
		n.ContentKey = s.ContentKey
		n.PathKey = s.PathKey
		if s.ContinuesID.Valid {
			n.Continues = t.node(s.ContinuesID.UUID)
		}
		parent := t.root
		if s.ParentID.Valid {
			parent = t.node(s.ParentID.UUID)
		}
		n.Parent = parent
		parent.Children = append(parent.Children, n)
		t.adopt(n, s.IDs)
	}
}

// LoadStartAliases restores aliases stored for dropped lines that stand for
// the start of the conversation (Alias.Node was nil when they were stored).
func (t *Transcript) LoadStartAliases(ids []NaturalID) {
	for _, id := range ids {
		t.aliases[id] = t.root
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

		if n := t.find(parent, continues, e); n != nil {
			seen := Seen{Index: i, Node: n, NewIDs: t.adopt(n, e.IDs), Continues: nil}
			if continues != nil && n.Continues == nil {
				n.Continues = continues
				seen.Continues = continues
			}
			res.Seen = append(res.Seen, seen)
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
		if l.Compaction {
			continue
		}
		target := t.root
		if l.To != nil {
			n, ok := t.lookup(*l.To)
			if !ok {
				continue
			}
			target = n
		}
		if prev, ok := t.aliases[l.ID]; ok && prev == target {
			continue
		}
		t.aliases[l.ID] = target
		alias := Alias{ID: l.ID, Node: target}
		if target.hidden {
			alias.Node = nil
		}
		res.Aliases = append(res.Aliases, alias)
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
// that have no entry. A compaction link anywhere on the way makes the entry
// a new tree root that continues from the link's target.
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
	target, cont, err := t.resolve(*l.To, links, depth+1)
	if err != nil {
		return nil, nil, err
	}
	if l.Compaction {
		return t.root, target, nil
	}
	return target, cont, nil
}

// find returns the stored node that e already is, or nil.
//
// A natural ID match anywhere in the transcript is certain. Otherwise the
// content key is matched among the children of parent, newest first, so an
// entry without IDs joins the branch in use now. A child does not match when
// it and e both have an ID of the same kind: two prompt IDs, or two
// transcript lines, are two entries even with the same text (a rewind).
// A new compaction root only matches a root that continues from the same
// old leaf, or a root whose continuation is not known yet.
func (t *Transcript) find(parent, continues *Node, e Entry) *Node {
	for _, id := range e.IDs {
		if n, ok := t.byID[id]; ok {
			return n
		}
	}
	key := ContentKey(e)
	for _, c := range slices.Backward(parent.Children) {
		if continues != nil && c.Continues != nil && c.Continues != continues {
			continue
		}
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
	n := blankNode(id)
	n.Parent = parent
	n.Continues = continues
	n.Entry = e
	n.Entry.IDs = nil
	n.ContentKey = key
	n.PathKey = PathKey(parent.PathKey, key)
	n.placeholder = false
	parent.Children = append(parent.Children, n)
	t.nodes[id] = n
	t.adopt(n, e.IDs)
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
		t.byID[id] = n
		k := idKey{kind: id.Kind, value: id.Value}
		if prev, ok := t.lastBlock[k]; !ok || id.Block >= blockOf(prev, k) {
			t.lastBlock[k] = n
		}
	}
	n.Entry.IDs = append(n.Entry.IDs, added...)
	return added
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

// node returns the node with this ID, creating a placeholder if the
// transcript has not seen it yet.
func (t *Transcript) node(id uuid.UUID) *Node {
	if n, ok := t.nodes[id]; ok {
		return n
	}
	n := blankNode(id)
	t.nodes[id] = n
	return n
}

func blankNode(id uuid.UUID) *Node {
	return &Node{
		ID:          id,
		Parent:      nil,
		Continues:   nil,
		Children:    nil,
		Entry:       Entry{IDs: nil, Parent: nil, Continues: nil, Synthetic: false, Role: "", Text: "", Call: nil, Result: nil, MessageID: "", At: zeroTime},
		ContentKey:  nil,
		PathKey:     nil,
		hidden:      false,
		placeholder: true,
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
