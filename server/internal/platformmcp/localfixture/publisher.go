//nolint:exhaustruct // In-memory fixture state initializes the fields it needs.
package localfixture

import (
	"context"
	"maps"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/plugins"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// InMemoryGitHubPublisher is a local-fixture implementation of the plugin
// publishing transport. It keeps generated package files in process memory and
// never contacts or represents a remote source-control destination.
type InMemoryGitHubPublisher struct {
	mu            sync.RWMutex
	files         map[string]map[string][]byte
	collaborators map[string]bool
}

var _ plugins.GitHubPublisher = (*InMemoryGitHubPublisher)(nil)

func NewInMemoryGitHubPublisher() *InMemoryGitHubPublisher {
	return &InMemoryGitHubPublisher{files: make(map[string]map[string][]byte), collaborators: make(map[string]bool)}
}

func (p *InMemoryGitHubPublisher) CreateRepo(_ context.Context, _ int64, owner, repo string, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := p.key(owner, repo, "main")
	if _, ok := p.files[key]; !ok {
		p.files[key] = make(map[string][]byte)
	}
	return nil
}

func (p *InMemoryGitHubPublisher) PushFiles(_ context.Context, _ int64, owner, repo, branch, _ string, files map[string][]byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files[p.key(owner, repo, branch)] = cloneFiles(files)
	return "local-fixture", nil
}

func (p *InMemoryGitHubPublisher) AddCollaborator(_ context.Context, _ int64, owner, repo, username, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.collaborators[p.collaboratorKey(owner, repo, username)] = true
	return nil
}

func (p *InMemoryGitHubPublisher) HasDirectCollaborator(_ context.Context, _ int64, owner, repo string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for key := range p.collaborators {
		if strings.HasPrefix(key, owner+"/"+repo+"/") {
			return true, nil
		}
	}
	return false, nil
}

func (p *InMemoryGitHubPublisher) GetRepoFiles(_ context.Context, _ int64, owner, repo, branch string) (map[string][]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	files, ok := p.files[p.key(owner, repo, branch)]
	if !ok {
		return nil, ghclient.ErrRepoNotFound
	}
	return cloneFiles(files), nil
}

func (p *InMemoryGitHubPublisher) GetFileContent(_ context.Context, _ int64, owner, repo, branch, path string) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	files, ok := p.files[p.key(owner, repo, branch)]
	if !ok {
		return nil, ghclient.ErrFileNotFound
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

func cloneFiles(files map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(files))
	for path, content := range files {
		cloned[path] = append([]byte(nil), content...)
	}
	return maps.Clone(cloned)
}
