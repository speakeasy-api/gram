package usersessions_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// sharedIssuerExpectedPin is where a shared issuer created on the test
// deployment is pinned: the server URL origin followed by the issuer's path.
func sharedIssuerExpectedPin(id string) string {
	return testServerURL + authserver.SharedPath(uuid.MustParse(id))
}

func TestCreateSharedIssuer_CreatesPinnedSharedIssuerWithGeneralPurposeDefaults(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "wizard-" + uuid.NewString()})
	require.NoError(t, err)

	require.Equal(t, authCtx.ActiveOrganizationID, created.OrganizationID)
	require.Empty(t, created.ProjectID)
	require.Equal(t, string(authserver.ModeShared), created.AuthorizationServerMode)
	require.NotNil(t, created.IssuerURL)
	require.Equal(t, sharedIssuerExpectedPin(created.ID), *created.IssuerURL)
	require.True(t, strings.HasPrefix(created.Slug, "shared-authorization-server-"), created.Slug)
	require.Equal(t, "interactive", created.AuthnChallengeMode)
	require.Equal(t, 336, created.SessionDurationHours)
	require.Equal(t, "open", created.ClientIDMetadataAdmissionMode)
	require.Nil(t, created.TrustedRemoteSessionIssuerID)
	require.Nil(t, created.TrustedRemoteSessionClientID)

	// Mode and pin are stored together, and the pin is exactly the issuer
	// the served authorization server derives for the row.
	row, err := repo.New(ti.conn).GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{ID: uuid.MustParse(created.ID), OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, string(authserver.ModeShared), row.AuthorizationServerMode)
	require.Equal(t, *created.IssuerURL, row.PinnedIssuerUrl.String)
	require.False(t, row.UseAuthenticationHost)
	served, err := (authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: "", PlatformHosts: nil}).SharedIssuerURL(row)
	require.NoError(t, err)
	require.Equal(t, *created.IssuerURL, served)

	loaded, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, created, loaded)

	createAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, createAudit.OrganizationID)
	require.False(t, createAudit.ProjectID.Valid)
	snapshot, err := audittest.DecodeAuditData(createAudit.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, created.ID, snapshot["ID"])
	require.Equal(t, string(authserver.ModeShared), snapshot["AuthorizationServerMode"])
	require.Equal(t, *created.IssuerURL, snapshot["IssuerURL"])
}

func TestCreateSharedIssuer_LeavesOrdinaryCreationInEndpointMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ordinary, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "ordinary",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	require.Equal(t, string(authserver.ModeEndpoint), ordinary.AuthorizationServerMode)
	require.Nil(t, ordinary.IssuerURL)
	require.Equal(t, "chain", ordinary.AuthnChallengeMode)
	require.Equal(t, 24, ordinary.SessionDurationHours)
}

func TestCreateSharedIssuer_RetryWithSameKeyReturnsSameIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	key := "retry-" + uuid.NewString()

	first, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
	require.NoError(t, err)
	second, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
	require.NoError(t, err)
	require.Equal(t, first, second)

	other, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "other-" + key})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, other.ID, "a different key creates a different issuer")
	require.NotEqual(t, *first.IssuerURL, *other.IssuerURL)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)

	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, int64(2), count, "a replay records no second creation")
}

func TestCreateSharedIssuer_ConcurrentSubmissionsWithSameKeyCreateOneIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	key := "concurrent-" + uuid.NewString()

	const submissions = 8
	ids := make([]string, submissions)
	group, groupCtx := errgroup.WithContext(ctx)
	for i := range submissions {
		group.Go(func() error {
			issuer, err := ti.service.CreateSharedIssuer(groupCtx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
			if err != nil {
				return fmt.Errorf("create shared issuer: %w", err)
			}
			ids[i] = issuer.ID
			return nil
		})
	}
	require.NoError(t, group.Wait())

	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestCreateSharedIssuer_KeyIsScopedToTheOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := "shared-key-" + uuid.NewString()

	mine, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
	require.NoError(t, err)

	otherOrganizationID := "org-" + uuid.NewString()
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrganizationID,
		Name:        "Other Organization",
		Slug:        "other-organization-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{String: otherOrganizationID, Valid: true},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	otherAuthCtx := *authCtx
	otherAuthCtx.ActiveOrganizationID = otherOrganizationID
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuthCtx)
	otherCtx = withExactAuthzGrants(t, otherCtx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, otherOrganizationID))

	theirs, err := ti.service.CreateSharedIssuer(otherCtx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
	require.NoError(t, err)
	require.NotEqual(t, mine.ID, theirs.ID, "the same key in another organization names another issuer")
	require.Equal(t, otherOrganizationID, theirs.OrganizationID)

	_, err = ti.service.GetIssuer(otherCtx, &orggen.GetIssuerPayload{ID: mine.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateSharedIssuer_RequiresOrgAdminUserSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err := ti.service.CreateSharedIssuer(readCtx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "forbidden"})
	requireOopsCode(t, err, oops.CodeForbidden)

	apiKeyAuthCtx := *authCtx
	apiKeyAuthCtx.APIKeyID = uuid.NewString()
	apiKeyCtx := contextvalues.SetAuthContext(ctx, &apiKeyAuthCtx)
	_, err = ti.service.CreateSharedIssuer(apiKeyCtx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "api-key"})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.CreateSharedIssuer(context.WithoutCancel(t.Context()), &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "anonymous"})
	requireOopsCode(t, err, oops.CodeUnauthorized)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Items)
}

func TestCreateSharedIssuer_RejectsBlankOrOverlongKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "   "})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: strings.Repeat("k", 201)})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateSharedIssuer_RefusesKeyOfDeletedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	key := "deleted-" + uuid.NewString()

	created, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil}))

	_, err = ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: key})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateSharedIssuer_RefusesWhenDeploymentServesNoSharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWith(t, testServiceConfig{hosts: &authserver.Hosts{ServerURL: "", AuthenticationHostBaseURL: "", PlatformHosts: nil}})

	_, err := ti.service.CreateSharedIssuer(ctx, &orggen.CreateSharedIssuerPayload{SessionToken: nil, IdempotencyKey: "unserved"})
	requireOopsCode(t, err, oops.CodeUnexpected)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Items, "nothing is written when no pin can be derived")
}
