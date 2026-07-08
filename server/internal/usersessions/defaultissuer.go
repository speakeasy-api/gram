package usersessions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// user_session_issuers.classification values.
const (
	// ClassificationCustom is a user-configured issuer.
	ClassificationCustom = "custom"
	// ClassificationProjectDefaultIDP is the auto-provisioned implicit Gram
	// issuer that gates private servers with no explicit issuer.
	ClassificationProjectDefaultIDP = "project_default_idp"
)

// defaultIssuerNamespace seeds the UUIDv5 derivation of every project's
// implicit issuer id. Fixed forever — changing it orphans previously minted
// tokens.
var defaultIssuerNamespace = uuid.MustParse("6f2b9a4e-3d1c-5f8a-9b0e-1a2c3d4e5f60")

const defaultIssuerSessionDuration = 30 * 24 * time.Hour

// DefaultIssuerID is the implicit project-default issuer's id — a pure
// function of the project, so runtime resolution derives the JWT audience
// without touching the database. The backing row (materialised lazily by
// GetOrCreateDefaultIssuer) only exists to satisfy the NOT NULL issuer FKs
// that OAuth writes carry; renaming or deleting it can't change what this
// returns.
func DefaultIssuerID(projectID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(defaultIssuerNamespace, projectID[:])
}

// defaultIssuerSlug is unique per project (it embeds the derived id) so the
// materialised row never collides with a user-created slug — no reservation
// guard needed.
func defaultIssuerSlug(projectID uuid.UUID) string {
	return "gram-default-" + DefaultIssuerID(projectID).String()
}

// GetOrCreateDefaultIssuer upserts the backing row for the project's implicit
// default issuer at its deterministic id, resurrecting it if soft-deleted.
// Called only from the stateful OAuth entry points (DCR, authorize/connect,
// dashboard mint) that need the row for their issuer FKs; resolution itself
// uses DefaultIssuerID and never reads it.
func GetOrCreateDefaultIssuer(ctx context.Context, db repo.DBTX, projectID uuid.UUID) (repo.UserSessionIssuer, error) {
	row, err := repo.New(db).UpsertDefaultUserSessionIssuer(ctx, repo.UpsertDefaultUserSessionIssuerParams{
		ID:                 DefaultIssuerID(projectID),
		ProjectID:          projectID,
		Slug:               defaultIssuerSlug(projectID),
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: defaultIssuerSessionDuration.Microseconds(),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	if err != nil {
		return repo.UserSessionIssuer{}, fmt.Errorf("upsert default user session issuer: %w", err)
	}
	return row, nil
}
