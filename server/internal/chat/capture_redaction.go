package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/captureredact"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// CaptureInput is original, transient evidence for a passive transcript copy.
// It is inspected before redaction and must never be persisted or published.
type CaptureInput struct {
	// OrgID scopes policy and fingerprint preparation.
	OrgID string
	// ProjectID scopes the captured message and policies.
	ProjectID uuid.UUID
	// ChatID anchors the captured session.
	ChatID uuid.UUID
	// MessageID anchors a message, or is zero for a content part.
	MessageID uuid.UUID
	// ContentPartID anchors an attachment, or is zero for a message.
	ContentPartID uuid.UUID
	// UserID attributes the captured traffic.
	UserID string
	// Source identifies the external agent surface.
	Source string
	// Role selects the existing risk message category.
	Role string
	// Content contains original text only during capture preparation.
	Content string
	// ToolCalls contains original arguments only during capture preparation.
	ToolCalls []byte
	// CreatedAt is the source event time.
	CreatedAt time.Time
}

// CaptureInspector preserves policy findings before credential values are
// removed from passive capture. Returned events must contain safe matches only.
type CaptureInspector interface {
	// InspectCapture prepares non-recoverable evidence before capture erases values.
	InspectCapture(context.Context, CaptureInput) ([]*riskv1.Finding, error)
}

// WithCaptureInspector enables safe finding preparation for passive captures.
func (w *ChatMessageWriter) WithCaptureInspector(inspector CaptureInspector) *ChatMessageWriter {
	w.captureInspector = inspector
	return w
}

func captureInputsFromRows(rows []chatMessageRow) []CaptureInput {
	inputs := make([]CaptureInput, len(rows))
	for i, row := range rows {
		inputs[i].Content = openrouter.GetText(row.content)
		inputs[i].ToolCalls = row.toolCalls
	}
	return inputs
}

func (w *ChatMessageWriter) inspectCaptureWrites(ctx context.Context, orgID string, writes []MessageWrite, original []CaptureInput) ([]*riskv1.Finding, error) {
	if w.captureInspector == nil {
		return nil, nil
	}
	if original != nil && len(original) != len(writes) {
		return nil, fmt.Errorf("capture evidence does not match messages")
	}
	var findings []*riskv1.Finding
	for i, write := range writes {
		p := write.Params
		if !shouldRedactCapture(p.Source.String, write.WorkloadSource) {
			continue
		}
		input := CaptureInput{
			OrgID: orgID, ProjectID: p.ProjectID, ChatID: p.ChatID, MessageID: p.ID,
			ContentPartID: uuid.Nil, UserID: p.UserID.String, Source: p.Source.String,
			Role: p.Role, Content: p.Content, ToolCalls: p.ToolCalls, CreatedAt: p.CreatedAt.Time,
		}
		if original != nil {
			input.Content, input.ToolCalls = original[i].Content, original[i].ToolCalls
		}
		prepared, err := w.captureInspector.InspectCapture(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("prepare capture findings: %w", err)
		}
		for _, finding := range prepared {
			if finding == nil || finding.HasMatch() || finding.GetSafeMatch() == nil ||
				finding.GetOrganizationId() != orgID || finding.GetProjectId() != p.ProjectID.String() || finding.GetChatMessageId() != p.ID.String() {
				return nil, fmt.Errorf("capture inspector returned unsafe finding")
			}
		}
		findings = append(findings, prepared...)
	}
	return findings, nil
}

func enqueueCaptureFindings(ctx context.Context, tx repo.DBTX, orgID string, findings []*riskv1.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	publications := make([]outbox.Message, 0, len(findings))
	for _, finding := range findings {
		publications = append(publications, outbox.Message{Proto: finding, PublicID: uuid.Nil, Attributes: nil})
	}
	if _, err := outbox.PublishBatch(ctx, tx, orgID, publications); err != nil {
		return fmt.Errorf("enqueue capture findings: %w", err)
	}
	return nil
}

// WriteCapturedContentParts inserts passive attachments and safe findings in
// their parent's transaction. Original content is used only for inspection;
// asset URLs must refer to the redacted bytes from WriteContentPartAssets.
func (w *ChatMessageWriter) WriteCapturedContentParts(ctx context.Context, tx repo.DBTX, orgID, userID string, parts []repo.CreateChatContentPartParams, original [][]byte) error {
	if original != nil && len(original) != len(parts) {
		return fmt.Errorf("capture evidence does not match content parts")
	}
	for i, part := range parts {
		if len(part.Metadata) > 0 && !json.Valid(part.Metadata) {
			return fmt.Errorf("captured content part metadata is invalid JSON")
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate captured content part ID: %w", err)
		}
		var findings []*riskv1.Finding
		if w.captureInspector != nil && original != nil {
			findings, err = w.captureInspector.InspectCapture(ctx, CaptureInput{
				OrgID: orgID, ProjectID: part.ProjectID, ChatID: part.ChatID, MessageID: uuid.Nil,
				ContentPartID: id, UserID: userID, Source: part.Source.String, Role: part.Kind,
				Content: string(original[i]), ToolCalls: nil, CreatedAt: part.CreatedAt.Time,
			})
			if err != nil {
				return fmt.Errorf("prepare captured content part findings: %w", err)
			}
			for _, finding := range findings {
				if finding == nil || finding.HasMatch() || finding.GetSafeMatch() == nil ||
					finding.GetOrganizationId() != orgID || finding.GetProjectId() != part.ProjectID.String() || finding.GetContentPartId() != id.String() {
					return fmt.Errorf("capture inspector returned unsafe content part finding")
				}
			}
		}
		if err := repo.New(tx).CreateCapturedChatContentPart(ctx, repo.CreateCapturedChatContentPartParams{
			ID: id, ChatID: part.ChatID, ProjectID: part.ProjectID, Kind: part.Kind,
			ContentAssetUrl: part.ContentAssetUrl, ExternalID: part.ExternalID, ParentChatMessageID: part.ParentChatMessageID,
			Version: part.Version, Source: part.Source, Metadata: captureredact.RedactJSON(ctx, part.Metadata),
			RiskAnalyzedAt: part.RiskAnalyzedAt, CreatedAt: part.CreatedAt,
		}); err != nil {
			return fmt.Errorf("insert captured content part: %w", err)
		}
		if err := enqueueCaptureFindings(ctx, tx, orgID, findings); err != nil {
			return err
		}
	}
	return nil
}

// isCodingCaptureSource identifies transcripts observed from external coding
// agents. Active Speakeasy assistant history is excluded: it is execution state,
// not an observation that can be changed independently of the next model turn.
func isCodingCaptureSource(source string) bool {
	switch CanonicalSource(source) {
	case "claude", "claude-code", "claude-code-web", "claude-code-desktop", "claude-tag", "cowork", "cursor", "codex", "codex-web", "opencode", "openclaw", "pi", "litellm":
		return true
	default:
		return false
	}
}

func shouldRedactCapture(source string, workload metering.WorkloadSource) bool {
	return workload == metering.WorkloadSourceHook || workload == metering.WorkloadSourceImport ||
		(workload == metering.WorkloadSourceNative && isCodingCaptureSource(source))
}

// A credential can span stream chunks, so captured text is announced only
// after the complete message has been inspected and stored safely.
func canTeeCompletionText(ctx context.Context, source billing.ModelUsageSource) bool {
	_, workload := messageReadingAttribution(ctx, source)
	return !shouldRedactCapture(string(source), workload)
}

func redactCaptureWrites(ctx context.Context, writes []MessageWrite) []MessageWrite {
	captured := append([]MessageWrite(nil), writes...)
	for i := range captured {
		write := &captured[i]
		if !shouldRedactCapture(write.Params.Source.String, write.WorkloadSource) {
			continue
		}
		if write.Params.ContentHash == nil {
			write.Params.ContentHash = captureContentHash(write.Params.Content)
		}
		content := redactCapturedContent(ctx, captureMessageContent{
			Content: write.Params.Content, ToolCalls: write.Params.ToolCalls, Raw: write.Params.ContentRaw,
		})
		write.Params.Content, write.Params.ContentRaw, write.Params.ToolCalls = content.Content, content.Raw, content.ToolCalls
	}
	return captured
}

type captureMessageContent struct {
	// Content is the flattened text used by transcript readers and scanners.
	Content string `json:"content"`
	// ToolCalls is the structured invocation payload, including nested arguments.
	ToolCalls json.RawMessage `json:"tool_calls"`
	// Raw is the richer representation stored inline or in a content blob.
	Raw json.RawMessage `json:"raw"`
}

// Inspect related fields together so credentials that need context, such as
// an AWS secret paired with an access identifier, cannot escape across fields.
func redactCapturedContent(ctx context.Context, content captureMessageContent) captureMessageContent {
	closed := captureMessageContent{Content: captureredact.Replacement, ToolCalls: nil, Raw: nil}
	if content.Raw != nil {
		closed.Raw = json.RawMessage(`"[redacted]"`)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return closed
	}
	var redacted captureMessageContent
	if json.Unmarshal(captureredact.RedactJSON(ctx, encoded), &redacted) != nil {
		return closed
	}
	if content.Raw == nil {
		redacted.Raw = nil
	}
	if content.ToolCalls == nil {
		redacted.ToolCalls = nil
	}
	return redacted
}

// captureHashPrefix distinguishes plain-text capture identity from provider
// import hashes that already use content_hash for their own replay alignment.
const captureHashPrefix = "capture:v1\x00"

func captureContentHash(content string) []byte {
	sum := sha256.Sum256([]byte(content))
	return append([]byte(captureHashPrefix), sum[:]...)
}

// CaptureContentDigest reads the original text digest from a capture identity.
// It distinguishes this format from provider-specific import hashes.
func CaptureContentDigest(contentHash []byte) ([]byte, bool) {
	if len(contentHash) != len(captureHashPrefix)+sha256.Size || !bytes.HasPrefix(contentHash, []byte(captureHashPrefix)) {
		return nil, false
	}
	return contentHash[len(captureHashPrefix):], true
}
