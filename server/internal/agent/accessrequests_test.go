package agent_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// attachAccessRequests wires a real access service into the agent service,
// with the access requests rollout on or off for the organization.
func attachAccessRequests(t *testing.T, ti *testInstance, enabled bool) {
	t.Helper()
	logger := testenv.NewLogger(t)
	siteURL, err := url.Parse(testSiteURL)
	require.NoError(t, err)
	orgHosts := orghost.New(orghost.Config{ServerURL: siteURL, SiteURL: siteURL})
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagAccessRequests, ti.orgID, enabled)
	service := access.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.chConn, ti.chConn, ti.sessions, nil, ti.authz, audit.NewLogger(), email.NewService(logger, nil, email.NewTemplateIDs(nil), false), orgHosts, nil).
		WithFeatures(flags)
	ti.service.WithAccessRequests(service)
}

// perUserCtx is the enrolled user's per-user device-agent key.
func perUserCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)
}

func grantOrgAdmin(t *testing.T, ctx context.Context, ti *testInstance, userID string) {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeOrgAdmin, ti.orgID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ti.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeOrgAdmin),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func TestListPendingAccessRequests_RefusesOrgInstallKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)

	// The default context is the org install key.
	_, err := ti.service.ListPendingAccessRequests(ctx, &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeForbidden)
}

func TestListPendingAccessRequests_RefusesAgentKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	agentCtx, _ := withAgentKeyAuth(t, ctx, ti, "CI agent")

	_, err := ti.service.ListPendingAccessRequests(agentCtx, &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeForbidden)
}

func TestListPendingAccessRequests_RefusesDeviceAgentToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})

	_, err := ti.service.JWTAuth(context.WithValue(t.Context(), goa.MethodKey, "listPendingAccessRequests"), device.token, deviceAgentTokenScheme)
	requireCode(t, err, oops.CodeForbidden)

	admitted, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)
	_, err = ti.service.ListPendingAccessRequests(admitted, &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeForbidden)
}

func TestListPendingAccessRequests_RefusesWorkloadActor(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	ctx = perUserCtx(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	workloadCtx := contextvalues.WithAuthenticatedActor(ctx, authCtx, urn.NewWorkloadPrincipal(uuid.New(), "device-1"))

	_, err := ti.service.ListPendingAccessRequests(workloadCtx, &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeForbidden)
}

func TestListPendingAccessRequests_NotFoundWhileRolloutOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, false)
	grantOrgAdmin(t, ctx, ti, mockidp.MockUserID)

	_, err := ti.service.ListPendingAccessRequests(perUserCtx(t, ctx), &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeNotFound)
}

func TestListPendingAccessRequests_NotFoundWithoutAccessService(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.ListPendingAccessRequests(perUserCtx(t, ctx), &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	requireCode(t, err, oops.CodeNotFound)
}

func TestListPendingAccessRequests_AdminSeesTheOrganizationsRequests(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	attachAccessRequests(t, ti, true)
	request := seedPendingRequest(t, ctx, ti, ti.orgID, "another-member")
	seedOrganization(t, ctx, ti, "other-access-org")
	seedPendingRequest(t, ctx, ti, "other-access-org", "stranger")

	res, err := ti.service.ListPendingAccessRequests(perUserCtx(t, ctx), &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	require.NoError(t, err)
	require.Empty(t, res.Requests, "someone who is not an administrator sees none")

	grantOrgAdmin(t, ctx, ti, mockidp.MockUserID)
	res, err = ti.service.ListPendingAccessRequests(perUserCtx(t, ctx), &gen.ListPendingAccessRequestsPayload{ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, res.Requests, 1, "another organization's requests never show")
	pending := res.Requests[0]
	require.Equal(t, request.String(), pending.ID)
	require.Equal(t, string(authz.ScopeChatRead), pending.Scope)
	require.Equal(t, 3600, *pending.RequestedDurationSeconds)
	require.NotEmpty(t, pending.PermissionLabel)
	require.Contains(t, pending.DecideURL, testSiteURL+"/")
	require.Contains(t, pending.DecideURL, "/access/requests?")
	require.Contains(t, pending.DecideURL, "request="+request.String())
}

func seedOrganization(t *testing.T, ctx context.Context, ti *testInstance, orgID string) {
	t.Helper()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: orgID, Slug: orgID})
	require.NoError(t, err)
}

// seedPendingRequest stores a pending chat:read request asking for an hour.
func seedPendingRequest(t *testing.T, ctx context.Context, ti *testInstance, orgID, requesterID string) uuid.UUID {
	t.Helper()
	selector, err := authz.NewSelector(authz.ScopeChatRead, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	row, err := accessrepo.New(ti.conn).InsertAccessRequest(ctx, accessrepo.InsertAccessRequestParams{
		OrganizationID:           orgID,
		RequesterUserID:          requesterID,
		Scope:                    string(authz.ScopeChatRead),
		Selector:                 selector,
		ResourceName:             pgtype.Text{String: "", Valid: false},
		Message:                  pgtype.Text{String: "Need it for an incident", Valid: true},
		ChallengeIds:             []string{},
		RequestedDurationSeconds: pgtype.Int4{Int32: 3600, Valid: true},
		RequestedUntil:           pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ExpiresAt:                pgtype.Timestamptz{Time: time.Now().Add(access.AccessRequestTTL), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return row.ID
}
