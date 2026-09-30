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
	"mime"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"cloud.google.com/go/storage"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// BlobReader reads immutable assets through the configured storage backend.
type BlobReader interface {
	Read(context.Context, *url.URL) (io.ReadCloser, error)
}

// permanentError contains a bounded reason, never message content or credentials.
type permanentError struct{ reason string }

func (e *permanentError) Error() string { return "sensor evaluation: " + e.reason }
func permanent(reason string) error     { return &permanentError{reason: reason} }

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func readReference(ctx context.Context, reader BlobReader, project string, ref *conversationv1.Message_ContentReference, remaining *int) ([]byte, error) {
	if ref == nil {
		return nil, permanent("invalid_reference")
	}
	u, err := url.Parse(ref.GetUri())
	if err != nil || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, permanent("invalid_reference")
	}
	// Backend bucket checks still apply. Explicit project paths also isolate local
	// filesystem reads and prevent another tenant's asset being evaluated.
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
		// FSBlobStore reads root-relative locators by stripping file://. Its
		// writer's relative file:path form becomes opaque after serialization.
		u = &url.URL{Scheme: "file", Host: project, Path: strings.TrimPrefix(p, project)}
	}
	if *remaining < 0 || ref.GetSizeBytes() > uint64(*remaining) {
		return nil, permanent("content_too_large")
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
	if ref.GetSizeBytes() != 0 && ref.GetSizeBytes() != uint64(len(data)) {
		return nil, permanent("asset_integrity")
	}
	if hash := ref.GetSha256(); len(hash) != 0 {
		sum := sha256.Sum256(data)
		if !bytes.Equal(hash, sum[:]) {
			return nil, permanent("asset_integrity")
		}
	}
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

func input(ctx context.Context, reader BlobReader, m *conversationv1.Message) (classifier.Entry, error) {
	var empty classifier.Entry
	remaining := maxContentBytes
	body := m.GetBody()
	if body == nil && m.GetBodyReference() != nil {
		data, err := readReference(ctx, reader, m.GetProjectId(), m.GetBodyReference(), &remaining)
		if err != nil {
			return empty, err
		}
		body = &conversationv1.Message_Body{}
		if proto.Unmarshal(data, body) != nil {
			return empty, permanent("invalid_body")
		}
	}
	if body == nil {
		return empty, permanent("missing_body")
	}
	if proto.Size(body) > maxContentBytes {
		return empty, permanent("content_too_large")
	}
	var content any
	hasOriginal := body.HasSourceContentJson() || body.HasSourceContent()
	if hasOriginal {
		data := body.GetSourceContentJson()
		if body.HasSourceContent() {
			var err error
			data, err = readReference(ctx, reader, m.GetProjectId(), body.GetSourceContent(), &remaining)
			if err != nil {
				return empty, err
			}
		}
		var err error
		content, err = jsonValue(data)
		if err != nil {
			return empty, err
		}
	}
	parts := make([]any, 0, len(body.GetParts()))
	for _, part := range body.GetParts() {
		switch {
		case part.HasText():
			if !hasOriginal {
				parts = append(parts, map[string]any{"text": part.GetText()})
			}
		case part.HasToolCall():
			call := part.GetToolCall()
			parts = append(parts, map[string]any{"tool_call": map[string]string{"id": call.GetId(), "name": call.GetName(), "arguments": call.GetArgumentsJson()}})
		case part.HasContentReference():
			ref := part.GetContentReference()
			media, _, err := mime.ParseMediaType(ref.GetMediaType())
			if err != nil || (!strings.HasPrefix(media, "text/") && media != "application/json") {
				return empty, permanent("unsupported_content")
			}
			data, err := readReference(ctx, reader, m.GetProjectId(), ref, &remaining)
			if err != nil {
				return empty, err
			}
			if !utf8.Valid(data) {
				return empty, permanent("invalid_content")
			}
			if media == "application/json" {
				value, err := jsonValue(data)
				if err != nil {
					return empty, err
				}
				parts = append(parts, map[string]any{"content": value})
			} else {
				parts = append(parts, map[string]any{"text": string(data)})
			}
		default:
			return empty, permanent("invalid_part")
		}
	}
	state := map[string]any{"role": m.GetRole().String(), "content": content, "parts": parts}
	data, err := json.Marshal(state)
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
