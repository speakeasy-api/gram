// Command transcript builds a transcript tree from one captured Claude
// session file and merges captures from other sources into it, in memory, so
// the transcript merge (server/internal/transcript) can be tried by hand. It
// touches no database.
//
//	go run ./cmd/transcript tree  <file>
//	go run ./cmd/transcript merge <first> <second> [more...]
//
// The input type is read from the file contents: a Claude Code session
// transcript (JSONL), Anthropic inference hook frames (JSONL, one frame per
// line), or an Anthropic Compliance API chat messages or local session
// messages page (JSON). See testdata/README.md for the seed scenarios and
// where each format comes from.
//
// The exit status is 0 when the tree has no possible duplicate, 1 when it
// has one, and 2 on a usage or input error.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const usage = `usage:
  transcript tree  <file>                      build a tree from one file and print it
  transcript merge <first> <second> [more...]  build a tree from the first file, then merge the others in, in order

Each file is a Claude Code session transcript (JSONL), Anthropic inference hook
frames (JSONL, one frame per line), or a Compliance API chat messages or local
session messages page (JSON).
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command and returns the process exit status.
func run(args []string, stdout, stderr io.Writer) int {
	var (
		sum summary
		err error
	)
	switch {
	case len(args) == 2 && args[0] == "tree":
		sum, err = treeCommand(stdout, args[1])
	case len(args) >= 3 && args[0] == "merge":
		sum, err = mergeCommand(stdout, args[1:])
	default:
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "transcript: %v\n", err)
		return 2
	}
	if sum.duplicates > 0 {
		return 1
	}
	return 0
}

// summary counts what a command did.
type summary struct {
	nodes int
	// matched and added count the entries of every file after the first.
	matched    int
	added      int
	duplicates int
}

// treeCommand builds a tree from one file and prints it.
func treeCommand(w io.Writer, path string) (summary, error) {
	in, err := readInput(path)
	if err != nil {
		return summary{nodes: 0, matched: 0, added: 0, duplicates: 0}, err
	}
	s := newSession()
	if _, err := s.merge(in); err != nil {
		return summary{nodes: 0, matched: 0, added: 0, duplicates: 0}, err
	}
	_, _ = fmt.Fprintf(w, "%s (%s, %s)\n", path, in.source, deliveryCount(in))
	printNotes(w, in)
	_, _ = io.WriteString(w, "\n")
	s.printTree(w)
	dups := s.duplicates()
	s.printDuplicates(w, dups)
	_, _ = fmt.Fprintf(w, "\n%d nodes · %d possible duplicates\n", len(s.nodes), len(dups))
	return summary{nodes: len(s.nodes), matched: 0, added: 0, duplicates: len(dups)}, nil
}

// mergeCommand builds a tree from the first file, merges the others into it
// in order, and prints what each entry became.
func mergeCommand(w io.Writer, paths []string) (summary, error) {
	if len(paths) < 2 {
		return summary{nodes: 0, matched: 0, added: 0, duplicates: 0}, errors.New("merge needs at least two files")
	}
	s := newSession()
	first, err := readInput(paths[0])
	if err != nil {
		return summary{nodes: 0, matched: 0, added: 0, duplicates: 0}, err
	}
	if _, err := s.merge(first); err != nil {
		return summary{nodes: 0, matched: 0, added: 0, duplicates: 0}, err
	}
	_, _ = fmt.Fprintf(w, "tree from %s (%s, %s): %d nodes\n", paths[0], first.source, deliveryCount(first), len(s.nodes))
	printNotes(w, first)

	sum := summary{nodes: 0, matched: 0, added: 0, duplicates: 0}
	for _, path := range paths[1:] {
		in, err := readInput(path)
		if err != nil {
			return sum, err
		}
		results, err := s.merge(in)
		if err != nil {
			return sum, err
		}
		_, _ = fmt.Fprintf(w, "\nmerge %s (%s, %s)\n", path, in.source, deliveryCount(in))
		printNotes(w, in)
		for _, r := range results {
			matched, added := s.printDelivery(w, r, len(in.deliveries) > 1)
			sum.matched += matched
			sum.added += added
		}
	}

	_, _ = io.WriteString(w, "\nfinal tree\n")
	s.printTree(w)
	dups := s.duplicates()
	s.printDuplicates(w, dups)
	sum.nodes = len(s.nodes)
	sum.duplicates = len(dups)
	_, _ = fmt.Fprintf(w, "\n%d matched · %d added · %d possible duplicates\n", sum.matched, sum.added, sum.duplicates)
	return sum, nil
}

func printNotes(w io.Writer, in *input) {
	for _, note := range in.notes {
		_, _ = fmt.Fprintf(w, "  note: %s\n", note)
	}
}

func deliveryCount(in *input) string {
	if in.source == sourceInference {
		return fmt.Sprintf("%d frames", len(in.deliveries))
	}
	if len(in.deliveries) == 1 {
		return "1 delivery"
	}
	return fmt.Sprintf("%d deliveries", len(in.deliveries))
}
