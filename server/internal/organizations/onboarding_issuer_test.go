package organizations_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func onboardingIssuers(t *testing.T, ctx context.Context, ti *testInstance) []usersessionsrepo.ListOrganizationUserSessionIssuersRow {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	rows, err := usersessionsrepo.New(ti.conn).ListOrganizationUserSessionIssuers(ctx, usersessionsrepo.ListOrganizationUserSessionIssuersParams{
		OrganizationID: ac.ActiveOrganizationID, IncludeOrganizational: true, IncludeProjectSpecific: false,
		Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: 10,
	})
	require.NoError(t, err)
	return rows
}

func TestOnboardingIssuerPreparesOnceWithoutEnablingGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	require.Empty(t, onboardingIssuers(t, ctx, ti))
	ac, _ := contextvalues.GetAuthContext(ctx)
	// Existing organizations which explicitly turned the gateway off stay off.
	_, err := featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)})
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	errors := make(chan error, 4)
	for range 4 {
		go func() { _, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{}); errors <- err }()
	}
	for range 4 {
		require.NoError(t, <-errors)
	}
	rows := onboardingIssuers(t, ctx, ti)
	require.Len(t, rows, 1)
	issuer := rows[0].UserSessionIssuer
	require.False(t, issuer.ProjectID.Valid)
	require.Equal(t, ac.ActiveOrganizationID, issuer.OrganizationID.String)
	require.Equal(t, "interactive", issuer.AuthnChallengeMode)
	require.Equal(t, (14 * 24 * time.Hour).Microseconds(), issuer.SessionDuration.Microseconds)
	require.Zero(t, issuer.SessionDuration.Months)
	require.Zero(t, issuer.SessionDuration.Days)
	require.Equal(t, "open", issuer.ClientIDMetadataAdmissionMode.String)
	require.False(t, issuer.TrustedRemoteSessionIssuerID.Valid)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	gateway, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})
	require.NoError(t, err)
	require.False(t, gateway.Available)
	require.Nil(t, gateway.URL)
}

func TestOnboardingIssuerPreservesExistingIssuers(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ctx, ti := newTestOrganizationsService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			for range count {
				createGatewayIssuer(t, ctx, ti, ac.ActiveOrganizationID)
			}
			before := onboardingIssuers(t, ctx, ti)
			_, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
			require.NoError(t, err)
			require.Equal(t, before, onboardingIssuers(t, ctx, ti))
		})
	}
}

func TestOnboardingIssuerSkipsHiddenConnectionStep(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	staff := *ac
	staff.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, &staff)
	_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "anthropic-admin-controls", Hidden: conv.PtrEmpty(true)})
	require.NoError(t, err)
	_, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{IncludeHidden: conv.PtrEmpty(true)})
	require.NoError(t, err)
	require.Empty(t, onboardingIssuers(t, ctx, ti))
}

func TestOnboardingIssuerKeepsUnconfiguredGatewayOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsServiceWithRealFeatures(t)
	_, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Len(t, onboardingIssuers(t, ctx, ti), 1)
	gateway, err := ti.service.GetGateway(ctx, &gen.GetGatewayPayload{})
	require.NoError(t, err)
	require.False(t, gateway.Available)
	require.Nil(t, gateway.URL)
}

func TestOnboardingIssuerSkipsAPIKeySessions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	keyAuth := *ac
	keyAuth.APIKeyID = "test-key"
	ctx = contextvalues.SetAuthContext(ctx, &keyAuth)
	_, _ = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.Empty(t, onboardingIssuers(t, ctx, ti))
}

func TestOnboardingIssuerLeavesProjectIssuerUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	require.NotNil(t, ac.ProjectID)
	q := usersessionsrepo.New(ti.conn)
	projectIssuer, err := q.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID: *ac.ProjectID, OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "existing-project-issuer", AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	_, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Len(t, onboardingIssuers(t, ctx, ti), 1)
	unchanged, err := q.GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: projectIssuer.ID, ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, projectIssuer, unchanged)
}

func TestOnboardingIssuerMemberReadsDoNotWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ac.ActiveOrganizationID))
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.NotNil(t, setupTask(result.Tasks, "anthropic-admin-controls"))
	require.Empty(t, onboardingIssuers(t, ctx, ti))
}

func TestOnboardingIssuerSupportReadsDoNotWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestOrganizationsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	support := *ac
	support.IsAdmin = true
	support.SupportOrganizationID = ac.ActiveOrganizationID
	ctx = contextvalues.WithValidatedSupportSession(ctx, &support)
	require.True(t, contextvalues.IsSupportSession(ctx))
	_, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.Empty(t, onboardingIssuers(t, ctx, ti))
}
