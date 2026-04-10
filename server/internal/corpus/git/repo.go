package git

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Repo wraps a bare git repository for corpus content operations.
type Repo struct {
	repo *gogit.Repository
	path string
}

// TreeEntry represents a file in a git tree.
type TreeEntry struct {
	Path string
	Size int64
}

// LogEntry represents a commit in the log.
type LogEntry struct {
	SHA     string
	Message string
}

// DiffAction represents the type of change in a diff.
type DiffAction int

const (
	DiffAdded    DiffAction = iota
	DiffModified DiffAction = iota
	DiffDeleted  DiffAction = iota
)

// DiffEntry represents a single file change between two commits.
type DiffEntry struct {
	Path   string
	Action DiffAction
}

// InitBareRepo initializes a new bare git repository at the given path.
func InitBareRepo(path string) (*Repo, error) {
	r, err := gogit.PlainInit(path, true)
	if err != nil {
		return nil, fmt.Errorf("init bare repo: %w", err)
	}

	return &Repo{repo: r, path: path}, nil
}

// OpenRepo opens an existing bare git repository at the given path.
func OpenRepo(path string) (*Repo, error) {
	r, err := gogit.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("open bare repo: %w", err)
	}

	return &Repo{repo: r, path: path}, nil
}

// Path returns the filesystem path of the bare repository.
func (r *Repo) Path() string {
	return r.path
}

// CommitFiles creates a commit with the given files replacing the entire tree.
// Returns the commit SHA.
func (r *Repo) CommitFiles(files map[string][]byte, message string) (string, error) {
	storer := r.repo.Storer

	rootTree, err := buildTree(storer, files)
	if err != nil {
		return "", fmt.Errorf("build tree: %w", err)
	}

	var parents []plumbing.Hash
	headRef, err := r.repo.Head()
	if err == nil {
		parents = append(parents, headRef.Hash())
	}

	sig := &object.Signature{
		Name:  "Gram Corpus",
		Email: "corpus@gram.dev",
		When:  time.Now(),
	}

	commit := &object.Commit{
		Hash:         plumbing.ZeroHash,
		Author:       *sig,
		Committer:    *sig,
		MergeTag:     "",
		Signature:    "",
		Message:      message,
		TreeHash:     rootTree,
		ParentHashes: parents,
		Encoding:     "",
		ExtraHeaders: nil,
	}

	obj := storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return "", fmt.Errorf("encode commit: %w", err)
	}

	commitHash, err := storer.SetEncodedObject(obj)
	if err != nil {
		return "", fmt.Errorf("store commit: %w", err)
	}

	ref := plumbing.NewHashReference(plumbing.Master, commitHash)
	if err := storer.SetReference(ref); err != nil {
		return "", fmt.Errorf("update HEAD: %w", err)
	}

	headSymRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)
	if err := storer.SetReference(headSymRef); err != nil {
		return "", fmt.Errorf("set HEAD symref: %w", err)
	}

	return commitHash.String(), nil
}

type dirEntry struct {
	name    string
	content []byte
}

func buildTree(storer gogitStorer, files map[string][]byte) (plumbing.Hash, error) {
	dirs := make(map[string][]dirEntry)
	for path, content := range files {
		dir, name := splitPath(path)
		dirs[dir] = append(dirs[dir], dirEntry{name: name, content: content})
	}

	return buildTreeRecursive(storer, dirs, "")
}

func buildTreeRecursive(storer gogitStorer, dirs map[string][]dirEntry, prefix string) (plumbing.Hash, error) {
	var entries []object.TreeEntry

	for _, entry := range dirs[prefix] {
		obj := storer.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		obj.SetSize(int64(len(entry.content)))

		w, err := obj.Writer()
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("blob writer: %w", err)
		}
		if _, err := w.Write(entry.content); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("write blob: %w", err)
		}
		if err := w.Close(); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("close blob: %w", err)
		}

		blobHash, err := storer.SetEncodedObject(obj)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("store blob: %w", err)
		}

		entries = append(entries, object.TreeEntry{
			Name: entry.name,
			Mode: 0o100644,
			Hash: blobHash,
		})
	}

	subdirs := make(map[string]bool)
	for dir := range dirs {
		if dir == prefix {
			continue
		}
		parent, name := splitPath(dir)
		if parent == prefix {
			subdirs[name] = true
		}
	}

	for subdir := range subdirs {
		var childPrefix string
		if prefix == "" {
			childPrefix = subdir
		} else {
			childPrefix = prefix + "/" + subdir
		}
		treeHash, err := buildTreeRecursive(storer, dirs, childPrefix)
		if err != nil {
			return plumbing.ZeroHash, err
		}

		entries = append(entries, object.TreeEntry{
			Name: subdir,
			Mode: 0o040000,
			Hash: treeHash,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		iName := entries[i].Name
		jName := entries[j].Name
		if entries[i].Mode == 0o040000 {
			iName += "/"
		}
		if entries[j].Mode == 0o040000 {
			jName += "/"
		}
		return iName < jName
	})

	tree := &object.Tree{Entries: entries, Hash: plumbing.ZeroHash}
	obj := storer.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode tree: %w", err)
	}

	treeHash, err := storer.SetEncodedObject(obj)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store tree: %w", err)
	}

	return treeHash, nil
}

func splitPath(path string) (dir, name string) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i], path[i+1:]
		}
	}
	return "", path
}

type gogitStorer interface {
	NewEncodedObject() plumbing.EncodedObject
	SetEncodedObject(plumbing.EncodedObject) (plumbing.Hash, error)
}

// ReadTree returns all file entries at the given ref.
func (r *Repo) ReadTree(ref string) ([]TreeEntry, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve ref %s: %w", ref, err)
	}

	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	var entries []TreeEntry
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("walk tree: %w", err)
		}
		if !entry.Mode.IsFile() {
			continue
		}
		entries = append(entries, TreeEntry{
			Path: name,
			Size: 0,
		})
	}

	return entries, nil
}

// ReadBlob returns the content of a file at the given ref and path.
func (r *Repo) ReadBlob(ref string, path string) ([]byte, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve ref %s: %w", ref, err)
	}

	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}

	file, err := commit.File(path)
	if err != nil {
		return nil, fmt.Errorf("get file %s: %w", path, err)
	}

	reader, err := file.Reader()
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	defer func() { _ = reader.Close() }()

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read content %s: %w", path, err)
	}

	return content, nil
}

// FileLog returns the commit log for a specific file path.
func (r *Repo) FileLog(path string) ([]LogEntry, error) {
	logIter, err := r.repo.Log(&gogit.LogOptions{
		From:       plumbing.ZeroHash,
		To:         plumbing.ZeroHash,
		Order:      gogit.LogOrderCommitterTime,
		FileName:   &path,
		PathFilter: nil,
		All:        false,
		Since:      nil,
		Until:      nil,
	})
	if err != nil {
		return nil, fmt.Errorf("git log for %s: %w", path, err)
	}
	defer logIter.Close()

	var entries []LogEntry
	err = logIter.ForEach(func(c *object.Commit) error {
		entries = append(entries, LogEntry{
			SHA:     c.Hash.String(),
			Message: c.Message,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterate log: %w", err)
	}

	return entries, nil
}

// Diff returns the file changes between two commits.
func (r *Repo) Diff(fromSHA, toSHA string) ([]DiffEntry, error) {
	fromCommit, err := r.repo.CommitObject(plumbing.NewHash(fromSHA))
	if err != nil {
		return nil, fmt.Errorf("get from commit %s: %w", fromSHA, err)
	}

	toCommit, err := r.repo.CommitObject(plumbing.NewHash(toSHA))
	if err != nil {
		return nil, fmt.Errorf("get to commit %s: %w", toSHA, err)
	}

	fromTree, err := fromCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get from tree: %w", err)
	}

	toTree, err := toCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get to tree: %w", err)
	}

	changes, err := fromTree.Diff(toTree)
	if err != nil {
		return nil, fmt.Errorf("compute diff: %w", err)
	}

	var entries []DiffEntry
	for _, change := range changes {
		var action DiffAction
		from := change.From
		to := change.To

		switch {
		case from.Name == "" && to.Name != "":
			action = DiffAdded
		case from.Name != "" && to.Name == "":
			action = DiffDeleted
		default:
			action = DiffModified
		}

		path := to.Name
		if path == "" {
			path = from.Name
		}

		entries = append(entries, DiffEntry{
			Path:   path,
			Action: action,
		})
	}

	return entries, nil
}
