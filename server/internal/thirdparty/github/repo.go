package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const apiBase = "https://api.github.com"

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

	url, _ := url.JoinPath(apiBase, "orgs", org, "repos")
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusUnprocessableEntity {
		return fmt.Errorf("create repo: status %d: %s", resp.StatusCode, truncatedBody(resp))
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

	commitTreeSHA, err := c.getCommitTree(ctx, installationID, owner, repo, headSHA)
	if err != nil {
		return "", fmt.Errorf("get commit tree: %w", err)
	}

	treeSHA, err := c.createTree(ctx, installationID, owner, repo, commitTreeSHA, files)
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

func (c *Client) getCommitTree(ctx context.Context, installationID int64, owner, repo, commitSHA string) (string, error) {
	url, _ := url.JoinPath(apiBase, "repos", owner, repo, "git", "commits", commitSHA)
	resp, err := c.doAPI(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncatedBody(resp))
	}

	var result struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode commit: %w", err)
	}
	return result.Tree.SHA, nil
}

func (c *Client) createTree(ctx context.Context, installationID int64, owner, repo, baseTreeSHA string, files map[string][]byte) (string, error) {
	treeEntries := make([]map[string]string, 0, len(files))
	for path, content := range files {
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

	payload := map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      treeEntries,
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
