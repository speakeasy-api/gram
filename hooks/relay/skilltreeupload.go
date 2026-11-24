package relay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// skillContentSchemaV2 is the upload contract that carries supporting
	// files.
	skillContentSchemaV2 = "hook.skill-content.v2"

	// uploadSkillContentPath is the skill content upload route.
	uploadSkillContentPath = "/rpc/hooks.uploadSkillContent"

	// maxSkillUploadResponseBytes bounds how much of an upload response body
	// is drained so the connection can be reused. Success responses are empty
	// and error bodies are small JSON documents.
	maxSkillUploadResponseBytes = 64 * 1024 // 64 KiB
)

// skillContentV2Body is the hook.skill-content.v2 request body. It is built
// here rather than through the generated SDK so the relay keeps the SDK's v1
// call as the fallback for servers that predate v2.
type skillContentV2Body struct {
	// SchemaVersion is always skillContentSchemaV2.
	SchemaVersion string `json:"schema_version"`

	// RawSHA256 is the lowercase SHA-256 of Content.
	RawSHA256 string `json:"raw_sha256"`

	// Content is the raw SKILL.md.
	Content string `json:"content"`

	// Files are the supporting files; never null.
	Files []skillContentV2File `json:"files"`
}

// skillContentV2File is one supporting file in a v2 upload.
type skillContentV2File struct {
	// Path is relative to the skill directory, slash-separated.
	Path string `json:"path"`

	// Content is the exact file bytes, base64-encoded on the wire.
	Content []byte `json:"content"`

	// SHA256 is the lowercase SHA-256 of Content.
	SHA256 string `json:"sha256"`
}

// skillUploadStatusError reports a definitive non-204 upload response.
type skillUploadStatusError struct {
	status int
}

func (e *skillUploadStatusError) Error() string {
	return fmt.Sprintf("skill upload returned HTTP %d", e.status)
}

// treeUploadRejected reports whether the server answered a v2 upload with a
// status a v1 upload may still get past: a server that predates v2 rejects
// the schema_version (400) or the body size (413), and a server that refuses
// a tree's files (400) still accepts its SKILL.md.
func treeUploadRejected(err error) bool {
	var statusErr *skillUploadStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	switch statusErr.status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusUnsupportedMediaType:
		return true
	default:
		return false
	}
}

// uploadSkillTree posts a hook.skill-content.v2 upload, gzip-compressed since
// skill trees are mostly text. Connection failures are retried like the v1
// upload; any HTTP answer is final.
func (cl *client) uploadSkillTree(ctx context.Context, c creds, task skillUploadTask) error {
	body := skillContentV2Body{
		SchemaVersion: skillContentSchemaV2,
		RawSHA256:     task.RawSHA256,
		Content:       task.Content,
		Files:         make([]skillContentV2File, 0, len(task.Files)),
	}
	for _, file := range task.Files {
		body.Files = append(body.Files, skillContentV2File{Path: file.Path, Content: file.Content, SHA256: file.SHA256})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode skill tree upload: %w", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(encoded); err != nil {
		return fmt.Errorf("compress skill tree upload: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("compress skill tree upload: %w", err)
	}

	target := strings.TrimRight(task.ServerURL, "/") + uploadSkillContentPath
	for attempt := 0; ; attempt++ {
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, attemptTimeout(ctx))
		status, err := postSkillTree(attemptCtx, cl.http, target, c, compressed.Bytes())
		cancelAttempt()
		if err == nil {
			if status == http.StatusNoContent {
				return nil
			}
			return &skillUploadStatusError{status: status}
		}
		if attempt >= sendAttempts-1 || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
}

func postSkillTree(ctx context.Context, httpClient *http.Client, target string, c creds, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build skill tree upload: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Gram-Key", c.APIKey)
	if c.Project != "" {
		req.Header.Set("Gram-Project", c.Project)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send skill tree upload: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxSkillUploadResponseBytes))
	_ = res.Body.Close()
	return res.StatusCode, nil
}

// validSkillTreeFiles checks files against the caps and digests a located
// tree satisfies, so a malformed task never reaches the network.
func validSkillTreeFiles(files []skillTreeFile) bool {
	if len(files) > maxSkillTreeFiles {
		return false
	}
	var total int
	for _, file := range files {
		total += len(file.Content)
		if !validSkillTreePath(file.Path) || len(file.Content) > maxSkillTreeFileBytes || total > maxSkillTreeTotalBytes {
			return false
		}
		digest := sha256.Sum256(file.Content)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			return false
		}
	}
	return true
}
