package git

import "errors"

// Repo wraps a bare git repository for corpus content operations.
type Repo struct{}

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
	return nil, errors.New("not implemented")
}

// OpenRepo opens an existing bare git repository at the given path.
func OpenRepo(path string) (*Repo, error) {
	return nil, errors.New("not implemented")
}

// CommitFiles creates a commit with the given files replacing the entire tree.
// Returns the commit SHA.
func (r *Repo) CommitFiles(files map[string][]byte, message string) (string, error) {
	return "", errors.New("not implemented")
}

// ReadTree returns all file entries at the given ref.
func (r *Repo) ReadTree(ref string) ([]TreeEntry, error) {
	return nil, errors.New("not implemented")
}

// ReadBlob returns the content of a file at the given ref and path.
func (r *Repo) ReadBlob(ref string, path string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

// FileLog returns the commit log for a specific file path.
func (r *Repo) FileLog(path string) ([]LogEntry, error) {
	return nil, errors.New("not implemented")
}

// Diff returns the file changes between two commits.
func (r *Repo) Diff(fromSHA, toSHA string) ([]DiffEntry, error) {
	return nil, errors.New("not implemented")
}
