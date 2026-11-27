package gram

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assets"
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

// transcriptShadow is the shadow store a process configured, and the projects
// that use it. Store is nil when no project captures shadows.
type transcriptShadow struct {
	// Store seals and opens shadow copies.
	Store *transcriptstore.Store

	// Assets seals and opens encrypted copies of message body, content part
	// and chat attachment objects for the same projects.
	Assets *transcriptstore.AssetStore

	// ProjectIDs are the projects that capture shadows and encrypted asset
	// copies. Their chat reads open them instead of plaintext, so a read never
	// meets a copy that capture stopped keeping current.
	ProjectIDs []uuid.UUID
}

// configureTranscriptShadow only opts explicit projects into encrypted shadow
// writes and reads. Plaintext remains authoritative for every other project;
// startup never backfills old messages.
func configureTranscriptShadow(ctx context.Context, c *cli.Context, db *pgxpool.Pool, writer *chat.ChatMessageWriter, assetStorage assets.BlobStore, orgEncryption orgEncryptionDeps, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (transcriptShadow, error) {
	none := transcriptShadow{Store: nil, Assets: nil, ProjectIDs: nil}
	values := c.StringSlice(chatTranscriptShadowProjectIDsFlag)
	if len(values) == 0 {
		// An explicitly supplied blank must not silently disable the feature.
		if c.IsSet(chatTranscriptShadowProjectIDsFlag) {
			return none, fmt.Errorf("chat transcript shadow requires explicit nonempty project UUIDs")
		}
		return none, nil
	}
	projectIDs, err := parseTranscriptShadowProjectIDs(values)
	if err != nil {
		return none, err
	}
	if orgEncryption.Service == nil || orgEncryption.Provisioner == nil {
		return none, fmt.Errorf("chat transcript shadow requires organization encryption service and provisioner")
	}
	if db == nil || writer == nil || assetStorage == nil {
		return none, fmt.Errorf("chat transcript shadow requires database, writer and asset storage")
	}
	queries := chatrepo.New(db)
	organizations := make([]string, 0, len(projectIDs))
	seen := make(map[string]struct{}, len(projectIDs))
	// Resolve every target before provisioning even the first organization's keys.
	for _, projectID := range projectIDs {
		org, err := queries.GetProjectOrganizationID(ctx, projectID)
		if err != nil {
			return none, fmt.Errorf("resolve chat transcript shadow project: %w", err)
		}
		if strings.TrimSpace(org) == "" {
			return none, fmt.Errorf("chat transcript shadow project has no organization")
		}
		if _, ok := seen[org]; !ok {
			organizations = append(organizations, org)
			seen[org] = struct{}{}
		}
	}
	// Build the store first: it can fail, and provisioning mints keys.
	store, err := transcriptstore.New(db, orgEncryption.Service, tracerProvider, meterProvider)
	if err != nil {
		return none, fmt.Errorf("create chat transcript shadow store: %w", err)
	}
	assetStore, err := transcriptstore.NewAssetStore(assetStorage, orgEncryption.Service, tracerProvider, meterProvider)
	if err != nil {
		return none, fmt.Errorf("create chat transcript shadow asset store: %w", err)
	}
	for _, org := range organizations {
		if err := orgEncryption.Provisioner.EnsureUseCase(ctx, org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return none, fmt.Errorf("provision chat transcript shadow encryption: %w", err)
		}
	}
	writer.WithTranscriptShadow(store, projectIDs...).WithSealedAssets(assetStore)
	// Captures run inside write transactions: keep the transcript key warm so
	// a request does not wait for KMS each time the cached key expires.
	go orgEncryption.Service.KeepWarm(ctx, orgencryption.UseCaseChatTranscripts)
	return transcriptShadow{Store: store, Assets: assetStore, ProjectIDs: projectIDs}, nil
}
