//nolint:exhaustruct // Local fixture state initializes only its selected backing store.
package localfixture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/plugins"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// InMemoryGitHubPublisher is a local-fixture implementation of the plugin
// publishing transport. By default it keeps generated package files in process
// memory. NewPersistentGitHubPublisher uses the same interface with a local
// filesystem backing store so the server and worker can share repositories and
// normal development restarts do not invalidate existing marketplace URLs.
// It never contacts or represents a remote source-control destination.
type InMemoryGitHubPublisher struct {
	mu            sync.RWMutex
	files         map[string]map[string][]byte
	collaborators map[string]bool
	root          string
}

var _ plugins.GitHubPublisher = (*InMemoryGitHubPublisher)(nil)

func NewInMemoryGitHubPublisher() *InMemoryGitHubPublisher {
	return &InMemoryGitHubPublisher{files: make(map[string]map[string][]byte), collaborators: make(map[string]bool), root: ""}
}

// NewPersistentGitHubPublisher stores local fixture repositories beneath root.
// Files are restricted because generated plugin packages may contain local API
// keys. Each snapshot is one atomically replaced JSON file, so separate server
// and worker processes never observe a partially written repository.
func NewPersistentGitHubPublisher(root string) (*InMemoryGitHubPublisher, error) {
	if root == "" {
		return nil, fmt.Errorf("persistent publisher root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create persistent publisher root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil { //nolint:gosec // Directories require execute permission; contents remain owner-only.
		return nil, fmt.Errorf("restrict persistent publisher root: %w", err)
	}
	if err := removeStalePublisherTemps(root, time.Now().Add(-time.Hour)); err != nil {
		return nil, err
	}
	return &InMemoryGitHubPublisher{files: nil, collaborators: nil, root: root}, nil
}

func removeStalePublisherTemps(root string, before time.Time) error {
	matches, err := filepath.Glob(filepath.Join(root, ".publisher-*.tmp"))
	if err != nil {
		return fmt.Errorf("match local fixture temporary files: %w", err)
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat local fixture temporary file: %w", err)
		}
		if !info.ModTime().Before(before) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove stale local fixture temporary file: %w", err)
		}
	}
	return nil
}

func (p *InMemoryGitHubPublisher) CreateRepo(_ context.Context, _ int64, owner, repo string, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := p.key(owner, repo, "main")
	if p.root == "" {
		if _, ok := p.files[key]; !ok {
			p.files[key] = make(map[string][]byte)
		}
		return nil
	}

	path := p.snapshotPath("repositories", key)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // Path is a SHA-256 filename beneath the configured local-only root.
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create local fixture repository: %w", err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("initialize local fixture repository: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close local fixture repository: %w", err)
	}
	return nil
}

func (p *InMemoryGitHubPublisher) PushFiles(_ context.Context, _ int64, owner, repo, branch, _ string, files map[string][]byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := p.key(owner, repo, branch)
	if p.root == "" {
		p.files[key] = cloneFiles(files)
		return "local-fixture", nil
	}
	if err := p.writeJSON(p.snapshotPath("repositories", key), cloneFiles(files)); err != nil {
		return "", fmt.Errorf("write local fixture repository: %w", err)
	}
	return "local-fixture", nil
}

func (p *InMemoryGitHubPublisher) AddCollaborator(_ context.Context, _ int64, owner, repo, username, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.root == "" {
		p.collaborators[p.collaboratorKey(owner, repo, username)] = true
		return nil
	}

	path := p.collaboratorPath(owner, repo, username)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // Path is a SHA-256 filename beneath the configured local-only root.
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create local fixture collaborator marker: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close local fixture collaborator marker: %w", err)
	}
	return nil
}

func (p *InMemoryGitHubPublisher) HasDirectCollaborator(_ context.Context, _ int64, owner, repo string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.root == "" {
		for key := range p.collaborators {
			if strings.HasPrefix(key, owner+"/"+repo+"/") {
				return true, nil
			}
		}
		return false, nil
	}

	matches, err := filepath.Glob(filepath.Join(p.root, "collaborator-"+hashKey(p.key(owner, repo, ""))+"-*.marker"))
	if err != nil {
		return false, fmt.Errorf("match local fixture collaborator markers: %w", err)
	}
	return len(matches) > 0, nil
}

func (p *InMemoryGitHubPublisher) GetRepoFiles(_ context.Context, _ int64, owner, repo, branch string) (map[string][]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	key := p.key(owner, repo, branch)
	if p.root == "" {
		files, ok := p.files[key]
		if !ok {
			return nil, ghclient.ErrRepoNotFound
		}
		return cloneFiles(files), nil
	}

	files := make(map[string][]byte)
	if err := p.readJSON(p.snapshotPath("repositories", key), &files); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ghclient.ErrRepoNotFound
		}
		return nil, fmt.Errorf("read local fixture repository: %w", err)
	}
	return cloneFiles(files), nil
}

// MainBranchFiles exposes the current local fixture snapshot to the local-only
// read-only Git HTTP server. It deliberately omits installation IDs because the
// in-memory publisher has no remote authorization boundary.
func (p *InMemoryGitHubPublisher) MainBranchFiles(ctx context.Context, owner, repo string) (map[string][]byte, error) {
	return p.GetRepoFiles(ctx, 0, owner, repo, "main")
}

func (p *InMemoryGitHubPublisher) GetFileContent(ctx context.Context, installationID int64, owner, repo, branch, path string) ([]byte, error) {
	files, err := p.GetRepoFiles(ctx, installationID, owner, repo, branch)
	if err != nil {
		if errors.Is(err, ghclient.ErrRepoNotFound) {
			return nil, ghclient.ErrFileNotFound
		}
		return nil, err
	}
	content, ok := files[path]
	if !ok {
		return nil, ghclient.ErrFileNotFound
	}
	return append([]byte(nil), content...), nil
}

func (p *InMemoryGitHubPublisher) key(owner, repo, branch string) string {
	return owner + "/" + repo + "/" + branch
}

func (p *InMemoryGitHubPublisher) collaboratorKey(owner, repo, username string) string {
	return owner + "/" + repo + "/" + username
}

func (p *InMemoryGitHubPublisher) snapshotPath(kind, key string) string {
	return filepath.Join(p.root, kind+"-"+hashKey(key)+".json")
}

func (p *InMemoryGitHubPublisher) collaboratorPath(owner, repo, username string) string {
	return filepath.Join(p.root, "collaborator-"+hashKey(p.key(owner, repo, ""))+"-"+hashKey(username)+".marker")
}

func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func (p *InMemoryGitHubPublisher) readJSON(path string, dst any) error {
	contents, err := os.ReadFile(path) //nolint:gosec // Path is a SHA-256 filename beneath the configured local-only root.
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(contents, dst); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

func (p *InMemoryGitHubPublisher) writeJSON(path string, value any) error {
	contents, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode local fixture state: %w", err)
	}
	contents = append(contents, '\n')

	tmp, err := os.CreateTemp(p.root, ".publisher-*.tmp")
	if err != nil {
		return fmt.Errorf("create local fixture temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restrict local fixture temporary file: %w", err)
	}
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write local fixture temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close local fixture temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace local fixture state: %w", err)
	}
	return nil
}

func cloneFiles(files map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(files))
	for path, content := range files {
		cloned[path] = append([]byte(nil), content...)
	}
	return cloned
}
