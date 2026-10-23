package auth_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func organizationIssuers(t *testing.T, ctx context.Context, inst *e2eInstance, organizationID string) []usersessionsrepo.ListOrganizationUserSessionIssuersRow {
	t.Helper()
	rows, err := usersessionsrepo.New(inst.conn).ListOrganizationUserSessionIssuers(ctx, usersessionsrepo.ListOrganizationUserSessionIssuersParams{
		OrganizationID: organizationID, IncludeOrganizational: true, IncludeProjectSpecific: false,
		Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: 10,
	})
	require.NoError(t, err)
	return rows
}

func TestRegisterOrganizationIssuerOnlyForNewOrganization(t *testing.T) {
	t.Parallel()
	for _, existingCount := range []int{-1, 0, 1, 2} {
		t.Run(fmt.Sprintf("existing_issuers_%d", existingCount), func(t *testing.T) {
			t.Parallel()
			const name = "Issuer Signup Fixture"
			const userID = "user_issuer_signup_fixture"
			fetcher := &mockWorkOSFetcher{members: map[string][]workos.Member{}, orgs: map[string]*workos.Organization{}}
			info := &MockUserInfo{UserID: userID, Email: "issuer-signup@example.test", Organizations: []MockOrganizationEntry{}}
			ctx, inst := newE2EAuthService(t, info, fetcher)
			callback, err := inst.callbackWithNonce(ctx, t)
			require.NoError(t, err)
			ctx, err = inst.sessionManager.Authenticate(ctx, callback.SessionToken)
			require.NoError(t, err)
			organizationID := orgid.FromWorkOSID("workos_org_" + name)
			// Simulate identity synchronization winning the metadata insert before
			// signup persists its newly provisioned WorkOS organization.
			if existingCount >= 0 {
				_, err := orgrepo.New(inst.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
					ID: organizationID, Name: name, Slug: "existing-issuer-signup", WorkosID: conv.ToPGText("workos_org_" + name),
				})
				require.NoError(t, err)
				for i := range existingCount {
					_, err := usersessionsrepo.New(inst.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
						OrganizationID: conv.ToPGText(organizationID), Slug: fmt.Sprintf("existing-%d", i), AuthnChallengeMode: "interactive",
						SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
					})
					require.NoError(t, err)
				}
			}
			before := organizationIssuers(t, ctx, inst, organizationID)
			beforeAudit, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionUserSessionIssuerCreate)
			require.NoError(t, err)
			require.NoError(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: name}))
			after := organizationIssuers(t, ctx, inst, organizationID)
			afterAudit, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionUserSessionIssuerCreate)
			require.NoError(t, err)
			if existingCount >= 0 {
				require.Equal(t, before, after, "adopting an existing org must not provision or change issuers")
				require.Equal(t, beforeAudit, afterAudit)
				return
			}
			require.Len(t, after, 1)
			issuer := after[0].UserSessionIssuer
			require.False(t, issuer.ProjectID.Valid)
			require.Equal(t, "interactive", issuer.AuthnChallengeMode)
			require.Equal(t, (14 * 24 * time.Hour).Microseconds(), issuer.SessionDuration.Microseconds)
			require.Zero(t, issuer.SessionDuration.Days)
			require.Zero(t, issuer.SessionDuration.Months)
			require.Equal(t, "open", issuer.ClientIDMetadataAdmissionMode.String)
			require.False(t, issuer.TrustedRemoteSessionIssuerID.Valid)
			require.False(t, issuer.TrustedRemoteSessionClientID.Valid)
			require.Equal(t, beforeAudit+1, afterAudit)
			org, err := orgrepo.New(inst.conn).GetOrganizationMetadata(ctx, organizationID)
			require.NoError(t, err)
			_, reason, err := orggateway.Availability(ctx, inst.conn, org)
			require.NoError(t, err)
			require.Equal(t, orggateway.UnavailabilityTurnedOff, reason, "creation must not opt into the gateway")
		})
	}
}

func TestRegisterOrganizationIssuerRollsBackWithOrganization(t *testing.T) {
	t.Parallel()
	const name = "Issuer Rollback Fixture"
	fetcher := &mockWorkOSFetcher{members: map[string][]workos.Member{}, orgs: map[string]*workos.Organization{}}
	info := &MockUserInfo{UserID: "user_issuer_rollback_fixture", Email: "issuer-rollback@example.test", Organizations: []MockOrganizationEntry{}}
	ctx, inst := newE2EAuthService(t, info, fetcher)
	callback, err := inst.callbackWithNonce(ctx, t)
	require.NoError(t, err)
	ctx, err = inst.sessionManager.Authenticate(ctx, callback.SessionToken)
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	inst.service.SetOrganizationFeatureSeederForTest(func(context.Context, pgx.Tx, string) error { return errors.New("fixture provisioning failure") })
	require.Error(t, inst.service.Register(ctx, &gen.RegisterPayload{OrgName: name}))
	organizationID := orgid.FromWorkOSID("workos_org_" + name)
	_, err = orgrepo.New(inst.conn).GetOrganizationMetadata(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, organizationIssuers(t, ctx, inst, organizationID))
	after, err := audittest.AuditLogCountByAction(ctx, inst.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
