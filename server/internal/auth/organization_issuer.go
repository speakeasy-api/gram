package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// createOrganizationIssuerTx runs only when signup inserts a new organization.
// It neither enables the organization gateway nor changes existing issuers.
func (s *Service) createOrganizationIssuerTx(ctx context.Context, tx pgx.Tx, organizationID, userID, email string) error {
	// Match server-created user-session issuers: interactive organization sign-in,
	// fourteen-day sessions, and the create query's explicit client-admission policy.
	const duration = 14 * 24 * time.Hour
	row, err := usersessionsrepo.New(tx).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(organizationID), Slug: "organization",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: duration.Microseconds(), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return fmt.Errorf("create user-session issuer: %w", err)
	}
	// This is a newly inserted default issuer, not a projection of arbitrary
	// existing policy. Keep its creation evidence in the organization transaction.
	if err := s.auditLogger.LogUserSessionIssuerCreate(ctx, tx, audit.LogUserSessionIssuerCreateEvent{
		OrganizationID: organizationID, ProjectID: uuid.Nil,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, userID), ActorDisplayName: conv.PtrEmpty(email), ActorSlug: nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(row.ID), Slug: row.Slug,
		UserSessionIssuerSnapshotAfter: &types.UserSessionIssuer{
			ID: row.ID.String(), ProjectID: "", OrganizationID: organizationID, Slug: row.Slug,
			AuthnChallengeMode: row.AuthnChallengeMode, SessionDurationHours: int(duration / time.Hour),
			ClientIDMetadataAdmissionMode: row.ClientIDMetadataAdmissionMode.String,
			TrustedRemoteSessionIssuerID:  nil, TrustedRemoteSessionClientID: nil,
			AuthorizationServerMode: "endpoint", UseAuthenticationHost: row.UseAuthenticationHost, IssuerURLPinned: false,
			IssuerURL: nil, TokenEndpoint: nil, AuthorizationServerUnavailableReason: nil,
			ProjectName: nil, AuthenticationHostAvailable: nil,
			CreatedAt: row.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339),
		},
	}); err != nil {
		return fmt.Errorf("audit organization user-session issuer creation: %w", err)
	}
	return nil
}
