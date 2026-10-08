package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	ahp "github.com/agenthooksprotocol/go-sdk"
	ahpserver "github.com/agenthooksprotocol/go-sdk/server"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const (
	ahpMaxContentBytes      = 64 << 10
	ahpMaxEventContentBytes = 256 << 10
	ahpContentTTL           = 10 * time.Minute
	ahpMaxConcurrentUploads = 32
	ahpMaxContentReferences = 128
	ahpContentReadTimeout   = time.Second
	ahpContentSlots         = 4096 // At most 256 MiB per authenticated scope per TTL.
)

type ahpCachedContent struct {
	Reference ahp.ContentReference
	Bytes     []byte
}

func ahpContentScope(ctx context.Context) (string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return "", fmt.Errorf("content authentication required")
	}
	principal := ahpPrincipalIdentity(ctx, authCtx)
	if principal == "" {
		return "", fmt.Errorf("content principal required")
	}
	return ahpIdentity(authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), principal), nil
}

func (s *Service) ahpUploadContent(w http.ResponseWriter, r *http.Request) {
	scope, err := ahpContentScope(r.Context())
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if s.cache == nil {
		http.Error(w, "Content receiver unavailable", http.StatusServiceUnavailable)
		return
	}
	s.ahpUploadsOnce.Do(func() { s.ahpUploads = make(chan struct{}, ahpMaxConcurrentUploads) })
	select {
	case s.ahpUploads <- struct{}{}:
		defer func() { <-s.ahpUploads }()
	default:
		http.Error(w, "Content receiver busy", http.StatusTooManyRequests)
		return
	}
	upload, err := ahpserver.ParseUpload(r, ahpMaxContentBytes)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ahpserver.ErrUploadSize) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "Invalid content upload", status)
		return
	}
	defer func() { _ = upload.Close() }()
	bytes, err := io.ReadAll(upload)
	if err != nil || !upload.Verified() || !utf8.Valid(bytes) {
		http.Error(w, "Invalid content upload", http.StatusBadRequest)
		return
	}
	// No overwrites: each verified publication receives a random opaque handle.
	ref, err := upload.Reference("ahp-content:" + uuid.NewString())
	if err != nil {
		http.Error(w, "Invalid content upload", http.StatusBadRequest)
		return
	}
	admitted := false
	admissionKey := ""
	random := uuid.New()
	start := (int(random[0])<<8 | int(random[1])) % ahpContentSlots
	stride := (int(random[2]) << 1) | 1
	for attempt := range 64 {
		slot := (start + attempt*stride) % ahpContentSlots
		key := fmt.Sprintf("ahp:content-slot:v1:%s:%d", scope, slot)
		added, err := s.cache.Add(r.Context(), key, ahpContentTTL)
		if err != nil {
			http.Error(w, "Content receiver unavailable", http.StatusServiceUnavailable)
			return
		}
		if added {
			admitted = true
			admissionKey = key
			break
		}
	}
	if !admitted {
		http.Error(w, "Content upload limit reached", http.StatusTooManyRequests)
		return
	}
	if err := s.cache.Set(r.Context(), "ahp:content:v1:"+scope+":"+ref.Ref, ahpCachedContent{Reference: ref, Bytes: bytes}, ahpContentTTL); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Second)
		defer cancel()
		_ = s.cache.Delete(cleanupCtx, admissionKey)
		http.Error(w, "Content receiver unavailable", http.StatusServiceUnavailable)
		return
	}
	_ = ahpserver.WriteUploadResponse(w, ref)
}

// resolveAHPContent walks only selected content items. refs are opaque cache
// handles: never URLs, filesystem paths or authenticated network destinations.
// Missing/unsupported bodies are a gap, not proof of an empty message.
func (s *Service) resolveAHPContent(ctx context.Context, event any) (map[string]string, bool) {
	resolved := map[string]string{}
	scope, err := ahpContentScope(ctx)
	b, encodeErr := json.Marshal(event)
	var root any
	if encodeErr != nil || json.Unmarshal(b, &root) != nil {
		return resolved, true
	}
	total := 0
	gaps := false
	aborted := false
	seen := map[string]bool{}
	contents := map[string]ahpCachedContent{}
	readCtx, cancel := context.WithTimeout(ctx, ahpContentReadTimeout)
	defer cancel()
	var walk func(any)
	walk = func(value any) {
		if aborted {
			return
		}
		switch v := value.(type) {
		case []any:
			for _, child := range v {
				if aborted {
					break
				}
				walk(child)
			}
		case map[string]any:
			if selection, ok := v["selection"].(string); ok {
				if selection != "body" {
					gaps = true
					return
				}
				media, _ := v["mediaType"].(string)
				kind, params, parseErr := mime.ParseMediaType(media)
				if parseErr != nil || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) || (!strings.HasPrefix(kind, "text/") && kind != "application/json") {
					gaps = true
					return
				}
				raw, ok := v["body"].(map[string]any)
				if !ok {
					gaps = true
					return
				}
				rb, _ := json.Marshal(raw)
				var ref ahp.ContentReference
				if json.Unmarshal(rb, &ref) != nil || !strings.HasPrefix(ref.Ref, "ahp-content:") {
					gaps = true
					return
				}
				if _, err := uuid.Parse(strings.TrimPrefix(ref.Ref, "ahp-content:")); err != nil {
					gaps = true
					return
				}
				if err != nil || s.cache == nil {
					gaps = true
					return
				}
				size, sizeErr := ref.Size.Float64()
				if sizeErr != nil || size < 0 || size > ahpMaxContentBytes {
					gaps = true
					return
				}
				if float64(total)+size+1 > ahpMaxEventContentBytes {
					gaps = true
					aborted = true
					return
				}
				content, found := contents[ref.Ref]
				if !found {
					if seen[ref.Ref] {
						gaps = true
						return
					}
					if len(seen) >= ahpMaxContentReferences {
						gaps = true
						aborted = true
						return
					}
					seen[ref.Ref] = true
					if s.cache.Get(readCtx, "ahp:content:v1:"+scope+":"+ref.Ref, &content) != nil {
						gaps = true
						if readCtx.Err() != nil {
							aborted = true
						}
						return
					}
					contents[ref.Ref] = content
				}
				digest := sha256.Sum256(content.Bytes)
				cachedSize, cachedSizeErr := content.Reference.Size.Float64()
				if outerSize, ok := v["size"].(float64); ok && outerSize != size {
					gaps = true
					return
				}
				if outerHash, ok := v["sha256"].(string); ok && outerHash != ref.Sha256 {
					gaps = true
					return
				}
				if sizeErr != nil || cachedSizeErr != nil || size != cachedSize || content.Reference.Ref != ref.Ref || content.Reference.Sha256 != ref.Sha256 || float64(len(content.Bytes)) != size || hex.EncodeToString(digest[:]) != ref.Sha256 || len(content.Bytes) > ahpMaxContentBytes || !utf8.Valid(content.Bytes) {
					gaps = true
					return
				}
				total += len(content.Bytes) + 1
				if total > ahpMaxEventContentBytes {
					aborted = true
					gaps = true
					return
				}
				resolved[ref.Ref] = string(content.Bytes)
				return
			}
		}
	}
	object, ok := root.(map[string]any)
	if !ok {
		return resolved, true
	}
	// Enumerate schema-owned descriptor slots. Tool input, model params,
	// permission suggestions, settings, errors and native extensions are opaque.
	for _, key := range []string{"items", "delta", "partialOutput", "summary", "instructions", "lastAssistantItem"} {
		walk(object[key])
	}
	for parent, keys := range map[string][]string{"message": {"text", "payload"}, "attention": {"message", "title"}, "elicitation": {"request", "result"}} {
		if nested, ok := object[parent].(map[string]any); ok {
			for _, key := range keys {
				walk(nested[key])
			}
		}
	}
	if changes, ok := object["fileChanges"].([]any); ok {
		for _, raw := range changes {
			if change, ok := raw.(map[string]any); ok {
				walk(change["before"])
				walk(change["after"])
			}
		}
	}
	if aborted {
		return map[string]string{}, true
	}
	return resolved, gaps
}
