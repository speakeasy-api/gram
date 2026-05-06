package marketplace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mirror manages local bare-repo mirrors of upstream private repos. Each
// upstream gets its own bare clone on disk; subsequent requests fetch into
// the existing mirror. Per-upstream mutexes prevent concurrent fetches from
// racing.
//
// The prototype shells out to `git`. Future revisions can swap in go-git
// without changing the public surface.
type Mirror struct {
	root   string
	logger *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewMirror(root string, logger *slog.Logger) *Mirror {
	return &Mirror{root: root, logger: logger, locks: map[string]*sync.Mutex{}}
}

func (m *Mirror) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}

// Ensure returns a path to a local bare mirror of the upstream that's been
// refreshed within the last `maxAge`. On first call it clones; on subsequent
// calls it fetches if the mirror is stale.
func (m *Mirror) Ensure(ctx context.Context, up Upstream, maxAge time.Duration) (string, error) {
	lock := m.lockFor(up.MirrorKey())
	lock.Lock()
	defer lock.Unlock()

	path := filepath.Join(m.root, up.MirrorKey()+".git")

	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat mirror: %w", err)
		}
		if err := m.clone(ctx, up, path); err != nil {
			return "", err
		}
		return path, nil
	}

	stale, err := m.isStale(path, maxAge)
	if err != nil {
		return "", err
	}
	if stale {
		if err := m.fetch(ctx, up, path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func (m *Mirror) clone(ctx context.Context, up Upstream, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir mirror parent: %w", err)
	}
	m.logger.InfoContext(ctx, "cloning mirror", slog.String("repo", up.Owner+"/"+up.Repo), slog.String("path", path))
	cmd := exec.CommandContext(ctx, "git", "clone", "--bare", up.CloneURL(), path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone --bare: %w: %s", err, redact(string(out), up.Auth.Password))
	}
	return m.touch(path)
}

func (m *Mirror) fetch(ctx context.Context, up Upstream, path string) error {
	m.logger.InfoContext(ctx, "refreshing mirror", slog.String("repo", up.Owner+"/"+up.Repo))
	// `git fetch --prune origin` against a bare clone updates refs/heads and refs/tags.
	// The remote URL embedded at clone time carries the credential; nothing to inject here.
	cmd := exec.CommandContext(ctx, "git", "-C", path, "fetch", "--prune", "origin",
		"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	// Re-set the remote URL on every fetch so a rotated PAT takes effect.
	if err := m.setRemoteURL(ctx, path, up.CloneURL()); err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch: %w: %s", err, redact(string(out), up.Auth.Password))
	}
	return m.touch(path)
}

func (m *Mirror) setRemoteURL(ctx context.Context, path, url string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "remote", "set-url", "origin", url)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote set-url: %w: %s", err, string(out))
	}
	return nil
}

const freshSentinel = ".gram-fetched-at"

func (m *Mirror) touch(path string) error {
	f, err := os.Create(filepath.Join(path, freshSentinel))
	if err != nil {
		return fmt.Errorf("touch sentinel: %w", err)
	}
	return f.Close()
}

func (m *Mirror) isStale(path string, maxAge time.Duration) (bool, error) {
	info, err := os.Stat(filepath.Join(path, freshSentinel))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("stat sentinel: %w", err)
	}
	return time.Since(info.ModTime()) > maxAge, nil
}

// ReadFileAtHead returns the contents of `relPath` at the mirror's HEAD ref.
// Used to read the published .claude-plugin/marketplace.json out of the mirror
// without checking out a working tree.
func (m *Mirror) ReadFileAtHead(ctx context.Context, mirrorPath, relPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", mirrorPath, "show", "HEAD:"+relPath)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("git show HEAD:%s: %w: %s", relPath, err, string(ee.Stderr))
		}
		return nil, fmt.Errorf("git show HEAD:%s: %w", relPath, err)
	}
	return out, nil
}

// redact strips a known secret from command output before it surfaces in an
// error. `git` itself rarely echoes the credential, but defense-in-depth.
func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}
