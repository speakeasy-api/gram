package main

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/transcript"
)

// session is one in-memory transcript plus what the CLI shows about its
// nodes: a short label and the source that added each one.
type session struct {
	tr *transcript.Transcript
	// nodes are every added node, oldest first.
	nodes  []*transcript.Node
	labels map[*transcript.Node]string
	origin map[*transcript.Node]transcript.Source
	counts map[string]int
	// cursors holds, per Compliance chat, the last message merged so far:
	// the importer fetches the chat's next page after it.
	cursors map[string]string
}

func newSession() *session {
	return &session{
		tr:      transcript.New(uuid.NewRandom),
		nodes:   nil,
		labels:  map[*transcript.Node]string{},
		origin:  map[*transcript.Node]transcript.Source{},
		counts:  map[string]int{},
		cursors: map[string]string{},
	}
}

// how an entry ended up in the tree.
type outcome struct {
	entry transcript.Entry
	node  *transcript.Node
	added bool
	// byID is the kind of the natural ID that matched, or "" for a match by
	// content key.
	byID      transcript.Kind
	newIDs    []transcript.NaturalID
	continues *transcript.Node
}

type deliveryResult struct {
	name     string
	skip     string
	outcomes []outcome
}

// merge merges every delivery of one input, in order.
func (s *session) merge(in *input) ([]deliveryResult, error) {
	results := make([]deliveryResult, 0, len(in.deliveries))
	for _, d := range in.deliveries {
		r := deliveryResult{name: d.name, skip: d.skip, outcomes: nil}
		if d.skip != "" {
			results = append(results, r)
			continue
		}
		delivery := s.continueCompliancePage(in, d.delivery)
		res, err := s.tr.Merge(delivery)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", in.path, d.name, err)
		}
		seen := make(map[int]transcript.Seen, len(res.Seen))
		for _, sn := range res.Seen {
			seen[sn.Index] = sn
		}
		added := res.Added
		for i, e := range delivery.Entries {
			if sn, ok := seen[i]; ok {
				r.outcomes = append(r.outcomes, outcome{
					entry:     e,
					node:      sn.Node,
					added:     false,
					byID:      matchedKind(e.IDs, sn.NewIDs),
					newIDs:    sn.NewIDs,
					continues: sn.Continues,
				})
				continue
			}
			n := added[0]
			added = added[1:]
			s.add(n, in.source)
			r.outcomes = append(r.outcomes, outcome{entry: e, node: n, added: true, byID: "", newIDs: nil, continues: nil})
		}
		results = append(results, r)
	}
	if in.chatID != "" && in.lastMessageID != "" {
		s.cursors[in.chatID] = in.lastMessageID
	}
	return results, nil
}

// continueCompliancePage makes a later page of a chat start after the last
// message merged from that chat, as the importer's after_id cursor does. The
// cursor itself (last_id) is opaque, so the page names the message instead.
func (s *session) continueCompliancePage(in *input, d transcript.Delivery) transcript.Delivery {
	last, ok := s.cursors[in.chatID]
	if in.chatID == "" || !ok || len(d.Entries) == 0 || d.Entries[0].Parent != nil {
		return d
	}
	d.Entries = slices.Clone(d.Entries)
	d.Entries[0].Parent = &transcript.NaturalID{Kind: transcript.KindComplianceMessage, Value: last, Block: transcript.LastBlock}
	return d
}

// matchedKind is the kind of the ID that matched a seen entry: the first of
// its IDs the node already had, which is the one find() looks up first. An
// entry whose IDs were all new matched by content key.
func matchedKind(ids, added []transcript.NaturalID) transcript.Kind {
	for _, id := range ids {
		if !slices.Contains(added, id) {
			return id.Kind
		}
	}
	return ""
}

func (s *session) add(n *transcript.Node, source transcript.Source) {
	prefix := kindPrefix(n.Entry)
	s.counts[prefix]++
	s.labels[n] = fmt.Sprintf("%s%d", prefix, s.counts[prefix])
	s.origin[n] = source
	s.nodes = append(s.nodes, n)
}

// kindPrefix is p (prompt or other user text), a (assistant text), t (tool
// call) or r (tool result).
func kindPrefix(e transcript.Entry) string {
	switch {
	case e.Call != nil:
		return "t"
	case e.Result != nil:
		return "r"
	case e.Role == transcript.RoleAssistant:
		return "a"
	default:
		return "p"
	}
}

func (s *session) label(n *transcript.Node) string {
	if l, ok := s.labels[n]; ok {
		return l
	}
	return "?"
}

// roots returns the tree roots, oldest first: the first prompt and every
// compaction summary.
func (s *session) roots() []*transcript.Node {
	if len(s.nodes) == 0 {
		return nil
	}
	top := s.nodes[0]
	for top.Parent != nil {
		top = top.Parent
	}
	return top.Children
}

// duplicate is a pair of siblings the merge should have matched: the same
// content key, and not both carrying an ID of the same kind (two prompt IDs
// or two transcript lines are two entries, as in a rewind).
type duplicate struct {
	parent *transcript.Node
	a, b   *transcript.Node
}

func (s *session) duplicates() []duplicate {
	var out []duplicate
	check := func(parent *transcript.Node, children []*transcript.Node) {
		for i, a := range children {
			for _, b := range children[i+1:] {
				if !bytes.Equal(a.ContentKey, b.ContentKey) || sameKind(a.Entry.IDs, b.Entry.IDs) {
					continue
				}
				// Compaction roots that continue different leaves are two trees.
				if a.Continues != nil && b.Continues != nil && a.Continues != b.Continues {
					continue
				}
				out = append(out, duplicate{parent: parent, a: a, b: b})
			}
		}
	}
	check(nil, s.roots())
	for _, n := range s.nodes {
		check(n, n.Children)
	}
	return out
}

func sameKind(a, b []transcript.NaturalID) bool {
	for _, x := range a {
		for _, y := range b {
			if x.Kind == y.Kind {
				return true
			}
		}
	}
	return false
}

// printTree prints every tree, one node per line. A run of single children
// stays at one indentation; only a branch indents.
func (s *session) printTree(w io.Writer) {
	for i, root := range s.roots() {
		if i > 0 {
			_, _ = io.WriteString(w, "\n")
		}
		s.printChain(w, root, "", "")
	}
}

func (s *session) printChain(w io.Writer, n *transcript.Node, first, rest string) {
	_, _ = fmt.Fprintf(w, "%s%s\n", first, s.describe(n))
	for len(n.Children) == 1 {
		n = n.Children[0]
		_, _ = fmt.Fprintf(w, "%s%s\n", rest, s.describe(n))
	}
	for i, c := range n.Children {
		branch, cont := "├─ ", "│  "
		if i == len(n.Children)-1 {
			branch, cont = "└─ ", "   "
		}
		s.printChain(w, c, rest+branch, rest+cont)
	}
}

// describe is a node's line: label, role, preview, the source that added
// it, and its flags.
func (s *session) describe(n *transcript.Node) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%-4s %-9s %s · %s", s.label(n), n.Entry.Role, pad(preview(n.Entry), previewWidth+2), s.origin[n])
	if n.Entry.Synthetic {
		b.WriteString(" [synthetic]")
	}
	if n.Continues != nil {
		_, _ = fmt.Fprintf(&b, " continues → %s", s.label(n.Continues))
	}
	return b.String()
}

func (s *session) printDuplicates(w io.Writer, dups []duplicate) {
	for _, d := range dups {
		where := "at the top"
		if d.parent != nil {
			where = "under " + s.label(d.parent)
		}
		_, _ = fmt.Fprintf(w, "! possible duplicate: %s and %s %s, %s\n", s.label(d.a), s.label(d.b), where, preview(d.a.Entry))
	}
}

// printDelivery prints one line per entry of a merged delivery and returns
// how many matched and how many were added.
func (s *session) printDelivery(w io.Writer, r deliveryResult, header bool) (matched, added int) {
	indent := "  "
	if header {
		_, _ = fmt.Fprintf(w, "  %s", r.name)
		if r.skip != "" {
			_, _ = fmt.Fprintf(w, ": %s", r.skip)
		}
		_, _ = io.WriteString(w, "\n")
		indent = "    "
	}
	for _, o := range r.outcomes {
		mark, how := "=", "by text"
		switch {
		case o.added:
			mark, how = "+", "added"
			added++
		case o.byID != "":
			how = fmt.Sprintf("by id (%s)", o.byID)
			matched++
		default:
			matched++
		}
		if len(o.newIDs) > 0 {
			how += ", IDs attached (" + kinds(o.newIDs) + ")"
		}
		if o.continues != nil {
			how += ", continues → " + s.label(o.continues) + " attached"
		}
		_, _ = fmt.Fprintf(w, "%s%s %-4s %s %s\n", indent, mark, s.label(o.node), pad(preview(o.entry), previewWidth+2), how)
	}
	return matched, added
}

func kinds(ids []transcript.NaturalID) string {
	var out []string
	for _, id := range ids {
		if !slices.Contains(out, string(id.Kind)) {
			out = append(out, string(id.Kind))
		}
	}
	return strings.Join(out, ", ")
}

const previewWidth = 44

// reminder matches injected <system-reminder> blocks, as in
// server/internal/transcript/key.go. Previews leave them out.
var reminder = regexp.MustCompile(`(?s)<system-reminder(?:\s[^>]*)?>.*?</system-reminder>`)

// preview is a short form of what an entry says: quoted text, the tool and
// call id of a tool call, or the call id and output of a tool result. Text
// that carried a system reminder is marked "+reminder".
func preview(e transcript.Entry) string {
	switch {
	case e.Call != nil:
		return e.Call.Name + " " + e.Call.ID
	case e.Result != nil:
		return e.Result.CallID + " " + quote(e.Result.Text, max(previewWidth-len(e.Result.CallID)-1, 14))
	default:
		return quote(e.Text, previewWidth)
	}
}

// quote cuts text to width runes, quotes included.
func quote(text string, width int) string {
	clean := reminder.ReplaceAllString(text, "")
	suffix := ""
	if clean != text {
		suffix = " +reminder"
	}
	clean = strings.Join(strings.Fields(clean), " ")
	if utf8.RuneCountInString(clean) > width-2 {
		clean = string([]rune(clean)[:width-3]) + "…"
	}
	return `"` + clean + `"` + suffix
}

func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
