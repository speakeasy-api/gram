package protectedresource

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

func recordDocument(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL string, doc wellknown.OAuthProtectedResourceMetadata, onlyIfChanged bool) error {
	return recordWithEMASync(ctx, db, projectID, orgID, resourceURL, doc.MetadataURL, func(tx pgx.Tx) error {
		// RFC 9728 requires exact resource and metadata-location equality.
		if !doc.ValidForResource(resourceURL) {
			return recordError(ctx, tx, projectID, orgID, resourceURL, doc.MetadataURL, mismatchMessage(resourceURL, doc))
		}
		// Invalid upstream JSON can abort PostgreSQL's transaction. Roll back only
		// the attempted document before recording its safe fetch-error fallback.
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("record protected resource: %w", err)
		}
		err = upsert(ctx, attempt, projectID, orgID, resourceURL, doc, onlyIfChanged)
		if err == nil {
			if err := attempt.Commit(ctx); err != nil {
				return fmt.Errorf("commit protected resource document: %w", err)
			}
			return nil
		}
		if rollbackErr := attempt.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		if isDataError(err) {
			return recordError(ctx, tx, projectID, orgID, resourceURL, doc.MetadataURL, "The metadata document contains values that cannot be stored.")
		}
		return fmt.Errorf("record protected resource: %w", err)
	})
}

// Diagnostic locations identify former sources that need reconciliation.
// They are not resource identities; document validation remains exact.
var metadataLocationPattern = regexp.MustCompile(`^(https?://[^/?#]+)(.*)$`)

func recordWithEMASync(ctx context.Context, db repo.DBTX, projectID uuid.UUID, orgID, resourceURL, metadataURL string, write func(pgx.Tx) error) error {
	beginner, ok := db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return errors.New("record protected resource: database does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin protected resource transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// This precedes the resource mutation, registration locks, and all source
	// reads. The writer scope includes shared-registration sibling projects.
	if err = authclientbackfill.LockEMAWriterScope(ctx, tx, orgID, []uuid.UUID{projectID}); err != nil {
		return fmt.Errorf("record protected resource: %w", err)
	}
	q := repo.New(tx)
	old, err := q.GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: projectID, ResourceIdentifier: resourceURL})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("record protected resource: %w", err)
	}
	oldMetadata := ""
	if err == nil {
		oldMetadata = old.MetadataUrl.String
	}
	sources, err := q.ListServersByProjectID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("record protected resource: %w", err)
	}
	affected := make([]uuid.UUID, 0)
	for _, source := range sources {
		location := strings.TrimRight(urls.DiagnosticURL(metadataLocationPattern.ReplaceAllString(source.Url, `${1}/.well-known/oauth-protected-resource${2}`)), "/")
		if source.Url == resourceURL ||
			location == strings.TrimRight(oldMetadata, "/") || location == strings.TrimRight(urls.DiagnosticURL(metadataURL), "/") {
			affected = append(affected, source.ID)
		}
	}
	if err = write(tx); err != nil {
		return fmt.Errorf("record protected resource: %w", err)
	}
	if err = authclientbackfill.SyncEMASources(ctx, tx, orgID, affected); err != nil {
		return fmt.Errorf("sync protected resource EMA reach: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit protected resource reconciliation: %w", err)
	}
	return nil
}
