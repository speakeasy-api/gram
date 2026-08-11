package assistants

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	assetssrv "github.com/speakeasy-api/gram/server/gen/http/assets/server"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// maxTurnAttachmentInlineBytes caps the total pre-base64 bytes inlined from
	// a dashboard turn's attachments, mirroring the Slack image budget.
	maxTurnAttachmentInlineBytes = 8 * 1024 * 1024
	// maxTurnAttachmentTextBytes caps how much of a single text-like attachment
	// is inlined as a text part. Larger files are truncated with a marker.
	maxTurnAttachmentTextBytes = 128 * 1024
	// turnAttachmentSignedURLTTL is how long the download URL handed to the
	// assistant for a file it cannot read inline stays valid.
	turnAttachmentSignedURLTTL = 30 * time.Minute
)

// SetAssetSigningKey wires the secret used to mint short-lived download URLs
// for turn attachments the runtime cannot read inline (PDFs, audio). Set after
// construction to match the existing injection pattern; when unset, those
// attachments are announced with metadata only.
func (s *ServiceCore) SetAssetSigningKey(key string) {
	s.assetSigningKey = key
}

// resolveDashboardTurnAttachments validates the caller's attachment ids against
// the project's chat attachment assets and returns the metadata the turn
// carries. Unknown, deleted, or cross-project ids fail the send rather than
// silently dropping a file the user believes the assistant can see.
func (s *ServiceCore) resolveDashboardTurnAttachments(ctx context.Context, projectID uuid.UUID, attachments []DashboardAttachmentInput) ([]dashboardTurnAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}

	ids := make([]uuid.UUID, 0, len(attachments))
	names := make(map[uuid.UUID]string, len(attachments))
	for _, attachment := range attachments {
		if _, seen := names[attachment.AssetID]; seen {
			continue
		}
		ids = append(ids, attachment.AssetID)
		names[attachment.AssetID] = attachment.Name
	}

	rows, err := assistantrepo.New(s.db).ListChatAttachmentAssets(ctx, assistantrepo.ListChatAttachmentAssetsParams{
		ProjectID: projectID,
		Ids:       ids,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve dashboard turn attachments: %w", err)
	}
	if len(rows) != len(ids) {
		return nil, ErrAssistantTurnAttachmentUnavailable
	}

	resolved := make([]dashboardTurnAttachment, 0, len(rows))
	byID := make(map[uuid.UUID]assistantrepo.ListChatAttachmentAssetsRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	// Preserve the caller's order so the turn reads in the order the user
	// attached the files.
	for _, id := range ids {
		row := byID[id]
		name := strings.TrimSpace(names[id])
		if name == "" {
			name = row.Name
		}
		resolved = append(resolved, dashboardTurnAttachment{
			AssetID:       row.ID,
			Name:          name,
			ContentType:   row.ContentType,
			ContentLength: row.ContentLength,
		})
	}
	return resolved, nil
}

// dashboardTurnAttachmentParts renders the attachments of a dashboard turn as
// runtime content parts: images ride along as image_url data URIs and text-like
// files are inlined as text. Everything else is left to the metadata block
// DecodeTurn already wrote. Strictly best-effort — any failure logs and drops
// that part rather than failing the turn.
func (s *ServiceCore) dashboardTurnAttachmentParts(ctx context.Context, projectID uuid.UUID, event assistantThreadEventRecord) []runtimeContentPart {
	if s.assetStorage == nil {
		return nil
	}

	var payload dashboardEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return nil
	}
	if len(payload.Attachments) == 0 {
		return nil
	}

	urls, err := s.chatAttachmentURLs(ctx, projectID, payload.Attachments)
	if err != nil {
		s.logger.WarnContext(ctx, "resolve dashboard turn attachment assets; sending metadata only", attr.SlogError(err))
		return nil
	}

	parts := make([]runtimeContentPart, 0, len(payload.Attachments))
	remaining := int64(maxTurnAttachmentInlineBytes)
	// Attachments whose bytes did not make it into the turn whole: the model
	// needs a URL for these, not a truncated excerpt.
	needsLink := make(map[uuid.UUID]struct{}, len(payload.Attachments))
	for _, attachment := range payload.Attachments {
		assetURL, ok := urls[attachment.AssetID]
		if !ok {
			continue
		}
		limit := attachmentInlineLimit(attachment.ContentType)
		if limit == 0 || attachment.ContentLength > remaining {
			needsLink[attachment.AssetID] = struct{}{}
			continue
		}

		data, err := blobio.ReadAllString(ctx, s.assetStorage, assetURL, limit)
		if err != nil {
			s.logger.WarnContext(ctx, "read dashboard turn attachment; sending metadata only",
				attr.SlogError(err), attr.SlogAssetID(attachment.AssetID.String()))
			needsLink[attachment.AssetID] = struct{}{}
			continue
		}
		remaining -= int64(len(data))
		if int64(len(data)) < attachment.ContentLength {
			needsLink[attachment.AssetID] = struct{}{}
		}

		if isInlineImageContentType(attachment.ContentType) {
			parts = append(parts, runtimeContentPart{
				Type: contentPartTypeImageURL,
				Text: "",
				ImageURL: &runtimeImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", attachment.ContentType, base64.StdEncoding.EncodeToString([]byte(data))),
				},
			})
			continue
		}

		truncated := ""
		if int64(len(data)) >= limit {
			truncated = "\n… (truncated)"
		}
		// `*-context` tag: Elements folds a leading context block into a
		// collapsed disclosure, so the file's bytes stay available to the model
		// without dumping the whole file into the user's own chat bubble.
		parts = append(parts, runtimeContentPart{
			Type:     contentPartTypeText,
			Text:     fmt.Sprintf("<attachment-context>\nname: %s\ntype: %s\n\n%s%s\n</attachment-context>", attachment.Name, attachment.ContentType, data, truncated),
			ImageURL: nil,
		})
	}
	// Files whose bytes are not in the turn — formats the completions protocol
	// cannot carry (PDFs, audio) and documents too large to inline (a full
	// OpenAPI spec) — are announced with a short-lived download URL so the
	// assistant can fetch them or hand them to a tool. Minted here rather than
	// in DecodeTurn because the prompt must stay byte-stable across replay.
	if links := s.dashboardTurnAttachmentLinks(ctx, projectID, payload.Attachments, needsLink); len(links) > 0 {
		var b strings.Builder
		b.WriteString("<attachment-downloads-context>\n")
		for _, attachment := range payload.Attachments {
			link, ok := links[attachment.AssetID]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "- %s (%s): %s\n", attachment.Name, attachment.ContentType, link)
		}
		b.WriteString("</attachment-downloads-context>")
		parts = append(parts, runtimeContentPart{Type: contentPartTypeText, Text: b.String(), ImageURL: nil})
	}

	if len(parts) == 0 {
		return nil
	}
	return parts
}

// chatAttachmentURLs re-resolves the storage URLs for a turn's attachments from
// the project's assets, so the runtime only ever reads blobs the project still
// owns.
func (s *ServiceCore) chatAttachmentURLs(ctx context.Context, projectID uuid.UUID, attachments []dashboardTurnAttachment) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.AssetID)
	}
	rows, err := assistantrepo.New(s.db).ListChatAttachmentAssets(ctx, assistantrepo.ListChatAttachmentAssetsParams{
		ProjectID: projectID,
		Ids:       ids,
	})
	if err != nil {
		return nil, fmt.Errorf("list chat attachment assets: %w", err)
	}
	urls := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		urls[row.ID] = row.Url
	}
	return urls, nil
}

// dashboardTurnAttachmentLinks mints short-lived download URLs for the given
// attachments, keyed by asset id, so the turn can hand the assistant something
// fetchable for files whose bytes it could not carry. Returns nil when no
// signing key is configured.
func (s *ServiceCore) dashboardTurnAttachmentLinks(ctx context.Context, projectID uuid.UUID, attachments []dashboardTurnAttachment, wanted map[uuid.UUID]struct{}) map[uuid.UUID]string {
	if s.assetSigningKey == "" || s.serverURL == nil || len(wanted) == 0 {
		return nil
	}
	links := make(map[uuid.UUID]string, len(wanted))
	for _, attachment := range attachments {
		if _, ok := wanted[attachment.AssetID]; !ok {
			continue
		}
		token, _, err := assets.GenerateSignedAssetToken(s.assetSigningKey, attachment.AssetID, projectID, turnAttachmentSignedURLTTL)
		if err != nil {
			s.logger.WarnContext(ctx, "mint dashboard turn attachment url; announcing metadata only",
				attr.SlogError(err), attr.SlogAssetID(attachment.AssetID.String()))
			continue
		}
		link := s.serverURL.JoinPath(assetssrv.ServeChatAttachmentSignedAssetsPath())
		link.RawQuery = url.Values{"token": {token}}.Encode()
		links[attachment.AssetID] = link.String()
	}
	return links
}

// attachmentInlineLimit reports how many bytes of an attachment of this content
// type may be inlined into a turn, or 0 when the runtime cannot read it inline.
func attachmentInlineLimit(contentType string) int64 {
	switch {
	case isInlineImageContentType(contentType):
		return maxTurnAttachmentInlineBytes
	case isInlineTextContentType(contentType):
		return maxTurnAttachmentTextBytes
	default:
		return 0
	}
}

// isInlineImageContentType matches the image formats the completions protocol
// carries as image_url content.
func isInlineImageContentType(contentType string) bool {
	switch baseMediaType(contentType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// isInlineTextContentType matches the files whose bytes are meaningful to the
// model as plain text.
func isInlineTextContentType(contentType string) bool {
	mediaType := baseMediaType(contentType)
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/yaml", "application/x-yaml":
		return true
	default:
		return false
	}
}

func baseMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return mediaType
}
