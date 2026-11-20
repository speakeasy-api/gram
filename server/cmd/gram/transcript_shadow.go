package gram

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// parseTranscriptShadowProjectIDs preserves an explicit, nonempty allowlist.
// Errors deliberately omit the supplied values.
func parseTranscriptShadowProjectIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for i, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || id == uuid.Nil {
			return nil, fmt.Errorf("invalid chat transcript shadow project UUID at position %d", i+1)
		}
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
			seen[id] = struct{}{}
		}
	}
	return ids, nil
}

// configureTranscriptShadow only opts explicit projects into encrypted shadow
// writes. Plaintext remains authoritative; startup never backfills old messages.
func configureTranscriptShadow(ctx context.Context, c *cli.Context, db *pgxpool.Pool, writer *chat.ChatMessageWriter, orgEncryption orgEncryptionDeps) error {
	values := c.StringSlice(chatTranscriptShadowProjectIDsFlag)
	if len(values) == 0 {
		// An explicitly supplied blank must not silently disable the feature.
		if c.IsSet(chatTranscriptShadowProjectIDsFlag) {
			return fmt.Errorf("chat transcript shadow requires explicit nonempty project UUIDs")
		}
		return nil
	}
	projectIDs, err := parseTranscriptShadowProjectIDs(values)
	if err != nil {
		return err
	}
	if orgEncryption.Service == nil || orgEncryption.Provisioner == nil {
		return fmt.Errorf("chat transcript shadow requires organization encryption service and provisioner")
	}
	if db == nil || writer == nil {
		return fmt.Errorf("chat transcript shadow requires database and writer")
	}
	// Schema availability is checked before any key provisioning calls.
	var schemaReady bool
	if err := db.QueryRow(ctx, "SELECT to_regclass('public.chat_message_transcripts') IS NOT NULL").Scan(&schemaReady); err != nil {
		return fmt.Errorf("check chat transcript shadow schema: %w", err)
	}
	if !schemaReady {
		return fmt.Errorf("chat transcript shadow schema is unavailable")
	}
	queries := chatrepo.New(db)
	organizations := make([]string, 0, len(projectIDs))
	seen := make(map[string]struct{}, len(projectIDs))
	// Resolve every target before provisioning even the first organization's keys.
	for _, projectID := range projectIDs {
		org, err := queries.GetProjectOrganizationID(ctx, projectID)
		if err != nil {
			return fmt.Errorf("resolve chat transcript shadow project: %w", err)
		}
		if strings.TrimSpace(org) == "" {
			return fmt.Errorf("chat transcript shadow project has no organization")
		}
		if _, ok := seen[org]; !ok {
			organizations = append(organizations, org)
			seen[org] = struct{}{}
		}
	}
	for _, org := range organizations {
		if err := orgEncryption.Provisioner.EnsureUseCase(ctx, org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return fmt.Errorf("provision chat transcript shadow encryption: %w", err)
		}
	}
	store, err := transcriptstore.New(db, orgEncryption.Service)
	if err != nil {
		return fmt.Errorf("create chat transcript shadow store: %w", err)
	}
	writer.WithTranscriptShadow(store, projectIDs...)
	return nil
}
