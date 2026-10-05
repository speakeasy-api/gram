package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"cloud.google.com/go/storage"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

// BlobReader reads immutable assets through the configured storage backend.
type BlobReader interface {
	Read(context.Context, *url.URL) (io.ReadCloser, error)
}

// permanentError contains a bounded reason, never message content or credentials.
type permanentError struct{ reason string }

func (e *permanentError) Error() string { return "sensor evaluation: " + e.reason }
func permanent(reason string) error     { return &permanentError{reason: reason} }
func digest(data []byte) string         { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func readReference(ctx context.Context, reader BlobReader, project, uri string, remaining *int) ([]byte, error) {
	u, err := url.Parse(uri)
	if err != nil || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, permanent("invalid_reference")
	}
	p := strings.TrimPrefix(u.Path, "/")
	if u.Scheme == "file" {
		if u.Opaque != "" {
			p, err = url.PathUnescape(u.Opaque)
			if err != nil {
				return nil, permanent("invalid_reference")
			}
		} else if u.Host != "" {
			p = u.Host + "/" + p
		}
	}
	if path.Clean(p) != p || !strings.HasPrefix(p, project+"/") {
		return nil, permanent("invalid_reference")
	}
	if u.Scheme != "gs" && u.Scheme != "s3" && u.Scheme != "file" {
		return nil, permanent("invalid_reference")
	}
	if u.Scheme == "file" {
		u = &url.URL{Scheme: "file", Host: project, Path: strings.TrimPrefix(p, project)}
	}
	if *remaining < 0 {
		return nil, permanent("content_too_large")
	}
	if reader == nil {
		return nil, fmt.Errorf("evaluation asset storage unavailable")
	}
	r, err := reader.Read(ctx, u)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrObjectNotExist) {
		return nil, permanent("missing_asset")
	}
	if err != nil {
		return nil, fmt.Errorf("read evaluation asset: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return r.Close() })
	data, err := io.ReadAll(io.LimitReader(r, int64(*remaining)+1))
	if err != nil {
		return nil, fmt.Errorf("read evaluation content: %w", err)
	}
	if len(data) > *remaining {
		return nil, permanent("content_too_large")
	}
	*remaining -= len(data)
	return data, nil
}

func jsonValue(data []byte) (any, error) {
	if !json.Valid(data) {
		return nil, permanent("invalid_content")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, permanent("invalid_content")
	}
	return value, nil
}

func input(ctx context.Context, reader BlobReader, project string, row repo.LoadEvaluationMessagesRow) (classifier.Entry, error) {
	var empty classifier.Entry
	m := row.ChatMessage
	remaining := maxContentBytes - len(m.Content) - len(m.ToolCalls)
	var content any
	hasOriginal := !row.RowLocalContent && (len(m.ContentRaw) > 0 || m.ContentAssetUrl.Valid && m.ContentAssetUrl.String != "")
	if hasOriginal {
		data := m.ContentRaw
		if len(data) == 0 {
			var err error
			data, err = readReference(ctx, reader, project, m.ContentAssetUrl.String, &remaining)
			if err != nil {
				return empty, err
			}
		} else {
			remaining -= len(data)
		}
		if remaining < 0 {
			return empty, permanent("content_too_large")
		}
		var err error
		content, err = jsonValue(data)
		if err != nil {
			return empty, err
		}
	}
	if remaining < 0 {
		return empty, permanent("content_too_large")
	}
	parts := make([]any, 0)
	if !hasOriginal && m.Content != "" {
		parts = append(parts, map[string]any{"text": m.Content})
	}
	if len(m.ToolCalls) > 0 {
		data := bytes.TrimSpace(m.ToolCalls)
		if len(data) > 0 && data[0] == '"' {
			var wrapped string
			if err := json.Unmarshal(data, &wrapped); err != nil {
				return empty, permanent("invalid_content")
			}
			data = []byte(wrapped)
		}
		var calls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		}
		if err := json.Unmarshal(data, &calls); err != nil {
			return empty, permanent("invalid_content")
		}
		for _, call := range calls {
			arguments := string(call.Function.Arguments)
			if len(call.Function.Arguments) > 0 && call.Function.Arguments[0] == '"' {
				if err := json.Unmarshal(call.Function.Arguments, &arguments); err != nil {
					return empty, permanent("invalid_content")
				}
			}
			parts = append(parts, map[string]any{"tool_call": map[string]string{"id": call.ID, "name": call.Function.Name, "arguments": arguments}})
		}
	}
	for _, uri := range row.AttachmentUris {
		data, err := readReference(ctx, reader, project, uri, &remaining)
		if err != nil {
			return empty, err
		}
		if !utf8.Valid(data) {
			return empty, permanent("invalid_content")
		}
		parts = append(parts, map[string]any{"text": string(data)})
	}
	data, err := json.Marshal(map[string]any{"role": "ROLE_" + strings.ToUpper(m.Role), "content": content, "parts": parts})
	if err != nil {
		return empty, permanent("invalid_content")
	}
	if len(data) > maxContentBytes {
		return empty, permanent("content_too_large")
	}
	entry, err := classifier.ParseEntry(data)
	if err != nil {
		return empty, permanent("invalid_content")
	}
	return entry, nil
}
