package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const apiBase = "https://api.github.com"

// CreateRepo creates a new repository under the given account.
// For organizations, uses the org repos endpoint. For user accounts, uses the
// authenticated user repos endpoint. If the repo already exists, this is a no-op.
func (c *Client) CreateRepo(ctx context.Context, installationID int64, owner, name string, private bool, accountType string) error {
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

	var url string
	if accountType == "Organization" {
		url = fmt.Sprintf("%s/orgs/%s/repos", apiBase, owner)
	} else {
		url = fmt.Sprintf("%s/user/repos", apiBase)
	}
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// 201 Created or 422 (already exists) are both acceptable.
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusUnprocessableEntity {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create repo: status %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// PushFiles atomically commits a set of files to the given repository branch.
// It uses the Git Trees API to create a single commit with all files, avoiding
// the need for a local git binary.
//
// files is a map of path -> content. Paths are relative to the repo root.
func (c *Client) PushFiles(ctx context.Context, installationID int64, owner, repo, branch, commitMsg string, files map[string][]byte) (string, error) {
	// 1. Get the current ref (branch HEAD).
	headSHA, err := c.getRef(ctx, installationID, owner, repo, branch)
	if err != nil {
		return "", fmt.Errorf("get ref: %w", err)
	}

	// 2. Get the tree SHA of the current commit.
	commitTreeSHA, err := c.getCommitTree(ctx, installationID, owner, repo, headSHA)
	if err != nil {
		return "", fmt.Errorf("get commit tree: %w", err)
	}

	// 3. Create a new tree with all files.
	treeSHA, err := c.createTree(ctx, installationID, owner, repo, commitTreeSHA, files)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	// 4. Create a commit pointing to the new tree.
	newCommitSHA, err := c.createCommit(ctx, installationID, owner, repo, commitMsg, treeSHA, headSHA)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	// 5. Update the branch ref to point to the new commit.
	if err := c.updateRef(ctx, installationID, owner, repo, branch, newCommitSHA); err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	return newCommitSHA, nil
}

func (c *Client) getRef(ctx context.Context, installationID int64, owner, repo, branch string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", apiBase, owner, repo, branch)
	resp, err := c.doAPI(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, body)
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
	url := fmt.Sprintf("%s/repos/%s/%s/git/commits/%s", apiBase, owner, repo, commitSHA)
	resp, err := c.doAPI(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, body)
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

	url := fmt.Sprintf("%s/repos/%s/%s/git/trees", apiBase, owner, repo)
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
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

	url := fmt.Sprintf("%s/repos/%s/%s/git/commits", apiBase, owner, repo)
	resp, err := c.doAPI(ctx, installationID, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
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
		"force": true, // We own this repo; force-update is safe.
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal ref update: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", apiBase, owner, repo, branch)
	resp, err := c.doAPI(ctx, installationID, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// Installation contains metadata about a GitHub App installation.
type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"` // "Organization" or "User"
	} `json:"account"`
}

// GetInstallation retrieves details about a GitHub App installation,
// including which org/user account it's installed on.
func (c *Client) GetInstallation(ctx context.Context, installationID int64) (*Installation, error) {
	appJWT, err := c.appJWT()
	if err != nil {
		return nil, fmt.Errorf("create app JWT: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d", apiBase, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get installation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get installation: status %d: %s", resp.StatusCode, body)
	}

	var inst Installation
	if err := json.NewDecoder(resp.Body).Decode(&inst); err != nil {
		return nil, fmt.Errorf("decode installation: %w", err)
	}
	return &inst, nil
}

// ListInstallations returns all installations of this GitHub App.
func (c *Client) ListInstallations(ctx context.Context) ([]Installation, error) {
	appJWT, err := c.appJWT()
	if err != nil {
		return nil, fmt.Errorf("create app JWT: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations", apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list installations: status %d: %s", resp.StatusCode, body)
	}

	var installations []Installation
	if err := json.NewDecoder(resp.Body).Decode(&installations); err != nil {
		return nil, fmt.Errorf("decode installations: %w", err)
	}
	return installations, nil
}
