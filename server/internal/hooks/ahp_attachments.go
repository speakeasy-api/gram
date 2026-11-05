package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	ahp "github.com/agenthooksprotocol/go-sdk"
	ahpserver "github.com/agenthooksprotocol/go-sdk/server"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const (
	// ahpUploadPath receives AHP content uploads. Harnesses upload attachment
	// bytes here before dispatching the event that references them.
	ahpUploadPath = "/ahp/uploads"
	// ahpMaxUploadBytes bounds a single staged attachment.
	ahpMaxUploadBytes = 1 << 20
	// Bound resolved attachment text and the final prompt-policy evidence.
	ahpMaxEvidenceBytes = 1 << 20
	// ahpUploadTTL bounds how long staged bytes wait for the referencing
	// event. Events follow their uploads within one dispatch, so this only
	// needs to cover retries.
	ahpUploadTTL = 15 * time.Minute
	// Limit upload admission across the fleet to a burst of 64 per project,
	// refilling 64 slots per staging TTL. Each upload is also size-bounded.
	ahpUploadBurst = 64
	// ahpUploadRefPrefix marks receiver references issued by this endpoint.
	ahpUploadRefPrefix = "urn:speakeasy:ahp:upload:"
	// ahpAttachmentKind is the prompt attachment kind for AHP attachments.
	ahpAttachmentKind = "file"
)

var errAHPEvidenceSize = errors.New("AHP prompt and attachment text exceeds the 1 MiB limit")

var errAHPAttachmentUnavailable = errors.New("AHP staged attachment unavailable")

// ahpStagedUpload is the verified upload body kept until an event resolves it.
type ahpStagedUpload struct {
	Content []byte `json:"content"`
}

// ahpUploadCacheKey scopes staged bytes to the uploading project, so a
// reference cannot be resolved from another project.
func ahpUploadCacheKey(projectID uuid.UUID, id string) string {
	return "ahp-upload:" + projectID.String() + ":" + id
}

// ahpUploadHandler serves AHP content uploads with the same authentication as
// the AHP event endpoint.
func (s *Service) ahpUploadHandler() http.Handler {
	return s.ahpAuthenticate(http.HandlerFunc(s.serveAHPUpload))
}

// serveAHPUpload verifies the declared size and digest, stages the bytes in
// the authenticated project, and only then confirms the receiver reference.
func (s *Service) serveAHPUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if s.cache == nil || s.ahpUploadLimiter == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	upload, err := ahpserver.ParseUpload(r, ahpMaxUploadBytes)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ahpserver.ErrUploadSize) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	// Reserve a project-scoped slot before reading or staging the body. Fail
	// closed when the shared quota store is unavailable.
	limit, err := s.ahpUploadLimiter.Allow(ctx, authCtx.ProjectID.String())
	if err != nil || !limit.Allowed {
		_ = upload.Close()
		status := http.StatusTooManyRequests
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to check AHP upload quota", attr.SlogError(err))
			status = http.StatusServiceUnavailable
		} else {
			seconds := max(1, int((limit.RetryAfter+time.Second-1)/time.Second))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	content, readErr := io.ReadAll(upload)
	closeErr := upload.Close()
	if readErr != nil || closeErr != nil || !upload.Verified() {
		status := http.StatusBadRequest
		if errors.Is(readErr, ahpserver.ErrUploadSize) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return
	}

	id := uuid.NewString()
	receipt, err := upload.Receipt(ahpUploadRefPrefix + id)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	// A receipt promises a resolvable reference, including for binary content.
	// Text-only filtering belongs at ingestion, not upload acceptance.
	staged := ahpStagedUpload{Content: content}
	if err := s.cache.Set(ctx, ahpUploadCacheKey(*authCtx.ProjectID, id), staged, ahpUploadTTL); err != nil {
		s.logger.ErrorContext(ctx, "failed to stage AHP upload",
			attr.SlogError(err),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	if err := ahpserver.WriteUploadResponse(w, receipt); err != nil {
		s.logger.WarnContext(ctx, "failed to write AHP upload response",
			attr.SlogError(err),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
	}
}

// ahpPromptAttachments resolves body-selected attachments on inbound user
// messages into prompt attachments. Only references issued by the upload
// endpoint in the authenticated project resolve. Binary bodies are staged but
// excluded from text prompt ingestion.
func (s *Service) ahpPromptAttachments(ctx context.Context, e *ahp.UserMessageInboundEvent) ([]*gen.HookPromptAttachmentEntry, error) {
	if e == nil {
		return nil, nil
	}
	authCtx, authenticated := contextvalues.GetAuthContext(ctx)
	var attachments []*gen.HookPromptAttachmentEntry
	evidenceBytes := 0
	for _, message := range e.Message.Messages {
		if message.Role != "user" {
			continue
		}
		for _, part := range message.Parts {
			if part == nil || !part.AttachmentBodyPart.Present || part.AttachmentBodyPart.Value == nil || part.AttachmentBodyPart.Value.Body == nil {
				continue
			}
			body := part.AttachmentBodyPart.Value
			id, ok := strings.CutPrefix(body.Body.Ref, ahpUploadRefPrefix)
			if !ok {
				continue
			}
			// A receipt promises staged evidence. Never turn an expired, evicted,
			// cross-project, or unreadable reference into a successful partial scan.
			// The SDK reports this error under the subscription's failure policy.
			if id == "" || s.cache == nil || !authenticated || authCtx == nil || authCtx.ProjectID == nil {
				return nil, errAHPAttachmentUnavailable
			}
			var staged ahpStagedUpload
			if err := s.cache.Get(ctx, ahpUploadCacheKey(*authCtx.ProjectID, id), &staged); err != nil {
				if !errors.Is(err, redisCache.ErrCacheMiss) {
					s.logger.WarnContext(ctx, "failed to resolve AHP attachment",
						attr.SlogError(err),
						attr.SlogProjectID(authCtx.ProjectID.String()),
					)
				}
				return nil, errAHPAttachmentUnavailable
			}
			if !ahpIsText(staged.Content) {
				continue
			}
			size := len(staged.Content)
			if len(attachments) > 0 {
				size++ // separator in the combined policy evidence
			}
			if size > ahpMaxEvidenceBytes-evidenceBytes {
				return nil, errAHPEvidenceSize
			}
			evidenceBytes += size
			timestamp := e.Time
			attachments = append(attachments, &gen.HookPromptAttachmentEntry{
				EntryUUID:      ahpAttachmentEntryID(e.Source, e.ID, message.ID, body.ID),
				PromptID:       nil,
				PromptSha256:   nil,
				FilePath:       nil,
				DisplayPath:    nil,
				AttachmentKind: ahpAttachmentKind,
				Content:        string(staged.Content),
				NumLines:       nil,
				TotalLines:     nil,
				StartLine:      nil,
				Timestamp:      &timestamp,
			})
		}
	}
	return attachments, nil
}

// ahpAttachmentEntryID derives a stable attachment identifier from the event
// and part identity, so replays of an event address the same attachment.
func ahpAttachmentEntryID(source, eventID, messageID, partID string) string {
	identity, _ := json.Marshal([]string{source, eventID, messageID, partID})
	return fmt.Sprintf("ahp-attachment:%x", sha256.Sum256(identity))
}

// ahpIsText reports whether content is UTF-8 text without NUL bytes.
func ahpIsText(content []byte) bool {
	return len(content) > 0 && utf8.Valid(content) && !bytes.ContainsRune(content, 0)
}
