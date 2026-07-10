package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const apiBase = "https://api.github.com"

// ErrRepoNotFound is returned by GetRepoFiles when the repository or the
// requested branch does not exist yet, so callers can treat a never-published
// project as a first publish rather than an error.
var ErrRepoNotFound = errors.New("github repo or branch not found")

// validGitHubUsername matches GitHub's username rules: 1-39 chars, starting
// with an alphanumeric, alphanumeric or hyphen thereafter. Enforced before
// using the value in URL construction to prevent path traversal via
// url.JoinPath, which resolves ../ segments in path elements.
var validGitHubUsername = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)

// maxErrBodyLen limits how much of a GitHub API error response is included
// in error messages to avoid leaking sensitive details into logs.
const maxErrBodyLen = 256

func truncatedBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyLen+1))
	s := string(body)
	if len(s) > maxErrBodyLen {
		s = s[:maxErrBodyLen] + "..."
	}
	return s
}

// CreateRepo creates a new repository under the given organization.
// If the repo already exists, this is a no-op.
func (c *Client) CreateRepo(ctx context.Context, installationID int64, org, name string, private bool) error {
	// Check if the repo already exists before attempting creation.
	// This avoids relying on parsing GitHub's 422 error prose.
	checkURL, _ := url.JoinPath(apiBase, "repos", org, name)
	checkResp, err := c.doAPI(ctx, installationID, http.MethodGet, checkURL, nil)
	if err != nil {
		return fmt.Errorf("check repo existence: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return checkResp.Body.Close() })
	if checkResp.StatusCode == http.StatusOK {
		return nil // repo already exists
	}

	payload := map[string]any{
		"name":          name,
		"private":       private,
		"auto_init":     true,
		"description":   "Plugin packages managed by Gram",
		"has_issues":    false,
		"has_wiki":      false,
		"has_downloads": false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal create repo: %w", err)
	}

	createURL, _ := url.JoinPath(apiBase, "orgs", org, "repos")
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, createURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create repo: status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	return nil
}

// AddCollaborator invites a GitHub user as a collaborator on the repo with
// the given permission level ("pull", "push", or "admin").
func (c *Client) AddCollaborator(ctx context.Context, installationID int64, owner, repo, username, permission string) error {
	if !validGitHubUsername.MatchString(username) {
		return fmt.Errorf("add collaborator: invalid username")
	}

	payload := map[string]any{
		"permission": permission,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal add collaborator: %w", err)
	}

	url, _ := url.JoinPath(apiBase, "repos", owner, repo, "collaborators", username)
	resp, err := c.doAPI(ctx, installationID, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("add collaborator: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// 201 = invite sent, 204 = already a collaborator
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("add collaborator: status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	return nil
}

// PushFiles atomically commits a set of files to the given repository branch
// using the Git Trees API.
func (c *Client) PushFiles(ctx context.Context, installationID int64, owner, repo, branch, commitMsg string, files map[string][]byte) (string, error) {
	headSHA, err := c.getRef(ctx, installationID, owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("get ref: %w", err)
	}

	treeSHA, err := c.createTree(ctx, installationID, owner, repo, files)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	newCommitSHA, err := c.createCommit(ctx, installationID, owner, repo, commitMsg, treeSHA, headSHA)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	if err := c.updateRef(ctx, installationID, owner, repo, branch, newCommitSHA); err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	return newCommitSHA, nil
}

// GetRepoFiles returns every file on the given branch as a path->content map,
// using the Git Trees API (recursive) plus one blob fetch per file. Returns
// ErrRepoNotFound if the repo or branch does not exist yet. The publish path
// uses this to carry an unchanged plugin component (hooks or MCP) verbatim from
// the existing repo into a fresh push, so publishing one component leaves the
// other's files — including their embedded API keys — untouched.
func (c *Client) GetRepoFiles(ctx context.Context, installationID int64, owner, repo, branch string) (map[string][]byte, error) {
	treeURL, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "trees", branch)
	treeURL += "?recursive=1"
	resp, err := c.doAPI(ctx, installationID, http.MethodGet, treeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusConflict {
		// 404: repo/branch absent. 409: repo exists but is empty (no commits on
		// the branch yet). Both mean "nothing published to carry forward".
		return nil, ErrRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get tree: status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	var tree struct {
		Entries []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("decode tree: %w", err)
	}
	if tree.Truncated {
		// Our plugin repos are far below GitHub's recursive listing limits;
		// a truncated response would silently drop files and corrupt a carry.
		return nil, fmt.Errorf("get tree: response truncated (repo too large)")
	}

	files := make(map[string][]byte, len(tree.Entries))
	for _, entry := range tree.Entries {
		if entry.Type != "blob" {
			continue
		}
		content, err := c.getBlob(ctx, installationID, owner, repo, entry.SHA)
		if err != nil {
			return nil, fmt.Errorf("get blob %s: %w", entry.Path, err)
		}
		files[entry.Path] = content
	}

	return files, nil
}

func (c *Client) getBlob(ctx context.Context, installationID int64, owner, repo, sha string) ([]byte, error) {
	blobURL, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "blobs", sha)
	resp, err := c.doAPI(ctx, installationID, http.MethodGet, blobURL, nil)
	if err != nil {
		return nil, err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blob); err != nil {
		return nil, fmt.Errorf("decode blob: %w", err)
	}
	if blob.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected blob encoding %q", blob.Encoding)
	}
	// The Blobs API wraps base64 content at column 76 with newlines, which the
	// standard decoder rejects; strip them before decoding.
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode base64 blob: %w", err)
	}
	return decoded, nil
}

func (c *Client) getRef(ctx context.Context, installationID int64, owner, repo, branch string) (string, error) {
	url, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "ref", "heads", branch)
	resp, err := c.doAPI(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode ref: %w", err)
	}
	return result.Object.SHA, nil
}

func (c *Client) createTree(ctx context.Context, installationID int64, owner, repo string, files map[string][]byte) (string, error) {
	treeEntries := make([]map[string]string, 0, len(files))
	for path, content := range files {
		// The Trees API treats `content` as UTF-8 text; binary content must be
		// uploaded via the Blobs API and referenced by `sha`. Fail loudly here
		// rather than silently corrupting binary files.
		if !utf8.Valid(content) {
			return "", fmt.Errorf("create tree: %q is not valid UTF-8 (binary content requires the Blobs API)", path)
		}
		mode := "100644"
		if strings.HasSuffix(path, ".sh") {
			mode = "100755"
		}
		treeEntries = append(treeEntries, map[string]string{
			"path":    path,
			"mode":    mode,
			"type":    "blob",
			"content": string(content),
		})
	}

	// Omit base_tree to build a clean tree from scratch. This ensures
	// deleted plugins are removed rather than orphaned in the repo.
	payload := map[string]any{
		"tree": treeEntries,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal tree: %w", err)
	}

	url, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "trees")
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode tree: %w", err)
	}
	return result.SHA, nil
}

func (c *Client) createCommit(ctx context.Context, installationID int64, owner, repo, message, treeSHA, parentSHA string) (string, error) {
	payload := map[string]any{
		"message": message,
		"tree":    treeSHA,
		"parents": []string{parentSHA},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal commit: %w", err)
	}

	url, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "commits")
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode commit: %w", err)
	}
	return result.SHA, nil
}

func (c *Client) updateRef(ctx context.Context, installationID int64, owner, repo, branch, commitSHA string) error {
	// force: true is required because we build clean trees (no base_tree)
	// which Git may see as non-fast-forward. Safe because each project gets
	// its own repo that Gram fully manages.
	payload := map[string]any{
		"sha":   commitSHA,
		"force": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ref update: %w", err)
	}

	url, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "refs", "heads", branch)
	resp, err := c.doAPI(ctx, installationID, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	return nil
}
