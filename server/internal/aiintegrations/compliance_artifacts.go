package aiintegrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
	"github.com/speakeasy-api/gram/server/internal/urn"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
)

const (
	// maxArtifactImportSize caps the size of a single Claude artifact we will
	// download and store. Mirrors the chat-attachment ceiling. Larger artifacts
	// are skipped with a logged warning rather than failing the import.
	maxArtifactImportSize = 10 * 1024 * 1024 // 10 MiB

	// Column length limits mirrored from the chat_artifacts CHECK constraints so
	// a pathological provider value cannot fail the insert.
	maxArtifactIDLength    = 300
	maxArtifactTitleLength = 1000
	maxArtifactTypeLength  = 200
)

// persistPageArtifacts stores every Claude artifact referenced by the messages
// in a fetched page. It is best-effort: a failure to download or store one
// artifact is logged and skipped so it never blocks the surrounding message
// import. The chat row already exists by the time this runs (the page-one
// enrich upserts it), so the chat_id foreign key is satisfied.
func (s *ComplianceImportService) persistPageArtifacts(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, page *anthropicapi.ChatMessagesPage) {
	if s.assetStorage == nil {
		return
	}

	for _, msg := range page.Messages {
		if msg.ID == "" || len(msg.Artifacts) == 0 {
			continue
		}
		for _, ref := range msg.Artifacts {
			if ref.ID == "" || ref.VersionID == "" {
				continue
			}
			if err := s.persistArtifact(ctx, client, cfg, chatID, msg.ID, ref); err != nil {
				s.logger.WarnContext(ctx, "failed to persist claude artifact",
					attr.SlogError(err),
					attr.SlogChatID(chatID.String()),
					attr.SlogProjectID(cfg.ProjectID.String()),
				)
			}
		}
	}
}

// persistArtifact downloads one artifact version, stores its bytes in the asset
// store (content-addressable, deduplicated by sha256), and upserts the
// chat_artifacts row. We keep only the latest version: the upsert swaps the
// stored asset and version metadata for the artifact's stable id.
func (s *ComplianceImportService) persistArtifact(ctx context.Context, client *anthropicapi.Client, cfg Config, chatID uuid.UUID, externalMessageID string, ref anthropicapi.ArtifactRef) error {
	downloaded, err := client.DownloadArtifact(ctx, ref.VersionID)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer func() { _ = downloaded.Body.Close() }()

	if downloaded.ContentLength > maxArtifactImportSize {
		s.logger.WarnContext(ctx, "skipping oversized claude artifact",
			attr.SlogChatID(chatID.String()),
			attr.SlogProjectID(cfg.ProjectID.String()),
		)
		return nil
	}

	// Read one byte past the cap so a content length that lies (or is absent)
	// still gets bounded.
	data, err := io.ReadAll(io.LimitReader(downloaded.Body, maxArtifactImportSize+1))
	if err != nil {
		return fmt.Errorf("read artifact content: %w", err)
	}
	if len(data) > maxArtifactImportSize {
		s.logger.WarnContext(ctx, "skipping oversized claude artifact",
			attr.SlogChatID(chatID.String()),
			attr.SlogProjectID(cfg.ProjectID.String()),
		)
		return nil
	}

	contentType := downloaded.ContentType
	if contentType == "" {
		contentType = ref.ArtifactType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	// Content-addressable upload. BlobStore.Write is idempotent for a given
	// path, so re-imports of identical content are no-ops, and CreateAsset
	// upserts on (project_id, sha256) — together they dedup across chats.
	objectPath := path.Join(cfg.ProjectID.String(), "artifacts", chatID.String(), hashHex)
	writer, assetURL, err := s.assetStorage.Write(ctx, objectPath, contentType, int64(len(data)))
	if err != nil {
		return fmt.Errorf("create artifact asset writer: %w", err)
	}
	if _, err := io.Copy(writer, bytes.NewReader(data)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write artifact content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize artifact upload: %w", err)
	}

	asset, err := assetsrepo.New(s.db).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          "artifact-" + hashHex,
		Url:           assetURL.String(),
		ProjectID:     cfg.ProjectID,
		Sha256:        hashHex,
		Kind:          string(urn.AssetKindArtifact),
		ContentType:   contentType,
		ContentLength: int64(len(data)),
	})
	if err != nil {
		return fmt.Errorf("create artifact asset: %w", err)
	}

	artifactType := ref.ArtifactType
	if artifactType == "" {
		artifactType = contentType
	}

	title := conv.ToPGTextEmpty(truncate(ref.Title, maxArtifactTitleLength))

	if _, err := chatrepo.New(s.db).UpsertChatArtifact(ctx, chatrepo.UpsertChatArtifactParams{
		ProjectID:          cfg.ProjectID,
		ChatID:             chatID,
		ExternalMessageID:  truncate(externalMessageID, maxArtifactIDLength),
		ExternalArtifactID: truncate(ref.ID, maxArtifactIDLength),
		ExternalVersionID:  truncate(ref.VersionID, maxArtifactIDLength),
		Title:              title,
		ArtifactType:       truncate(artifactType, maxArtifactTypeLength),
		AssetID:            asset.ID,
	}); err != nil {
		return fmt.Errorf("upsert chat artifact: %w", err)
	}

	return nil
}

// truncate bounds a provider-supplied string to the column's CHECK limit. The
// values are identifiers and titles that are well under these limits in
// practice; this is purely defensive so a malformed value cannot fail the
// insert.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
