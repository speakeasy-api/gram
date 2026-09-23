package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// workloadIdentityOrg creates an organization with one agent, and returns the
// agent id the admission path assigns.
func workloadIdentityOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, slug string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: orgID, Slug: slug, GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))
	// An agent must have an owner who is a member of its organization, so the
	// membership row comes first.
	ownerUserID := orgID + "_owner"
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID, UserID: conv.ToPGText(ownerUserID),
	}))
	agentID := uuid.New()
	_, err := testrepo.New(conn).CreateAttachmentAgentFixture(ctx, testrepo.CreateAttachmentAgentFixtureParams{
		ID: agentID, OrganizationID: orgID, OwnerUserID: ownerUserID, Name: "poc-agent",
	})
	require.NoError(t, err)
	return agentID
}

// These endpoints hand a machine an agent's authority, so an unauthenticated
// caller must not reach them. This also pins that every route is mounted.
func TestWorkloadIdentityRequiresAdminSession(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-workload-identity-auth", "operator@example.com")))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)

	// The requests are well formed, so a 401 is the security scheme refusing
	// them rather than the decoder rejecting a missing field.
	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/admin/organization.workloadIdentity?organization_id=org_x", body: ""},
		{method: http.MethodPost, path: "/admin/organization.workloadIssuer", body: `{"organization_id":"org_x","name":"n","issuer":"https://i.example","jwks_uri":"https://i.example/jwks.json"}`},
		{method: http.MethodPost, path: "/admin/organization.workloadAdmission", body: `{"organization_id":"org_x","workload_issuer_id":"` + uuid.New().String() + `","subject":"s","agent_id":"` + uuid.New().String() + `"}`},
		{method: http.MethodPost, path: "/admin/organization.workloadAuthenticationHost", body: `{"organization_id":"org_x","user_session_issuer_id":"` + uuid.New().String() + `","enabled":true}`},
		{method: http.MethodPost, path: "/admin/organization.workloadIdentityTeardown", body: `{"organization_id":"org_x","workload_issuer_id":"` + uuid.New().String() + `"}`},
	} {
		var body io.Reader
		if test.body != "" {
			body = strings.NewReader(test.body)
		}
		req := httptest.NewRequest(test.method, test.path, body)
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s: %s", test.method, test.path, rec.Body.String())
	}
}

func TestWorkloadIdentity_CreateIssuerThenAdmitSubject(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_wi_create"
	agentID := workloadIdentityOrg(t, ctx, conn, orgID, "wi-create")

	created, err := svc.CreateWorkloadIssuer(ctx, &gen.CreateWorkloadIssuerPayload{
		OrganizationID: orgID,
		Name:           "anthropic",
		Issuer:         "https://issuer.example/agents",
		JwksURI:        "https://issuer.example/agents/jwks.json",
	})
	require.NoError(t, err)
	require.Len(t, created.Issuers, 1)
	require.Equal(t, "https://issuer.example/agents", created.Issuers[0].Issuer)
	require.Empty(t, created.Subjects)

	admitted, err := svc.AdmitWorkloadSubject(ctx, &gen.AdmitWorkloadSubjectPayload{
		OrganizationID:   orgID,
		WorkloadIssuerID: created.Issuers[0].ID,
		Subject:          "wimse://issuer.example/org/o/agent/a",
		AgentID:          agentID.String(),
	})
	require.NoError(t, err)
	require.Len(t, admitted.Subjects, 1)
	require.Equal(t, "wimse://issuer.example/org/o/agent/a", admitted.Subjects[0].Subject)
	// The agent is what supplies the workload's policy, so an admission that
	// reports no agent is the half-configured state the grant refuses.
	require.NotNil(t, admitted.Subjects[0].AgentID)
	require.Equal(t, agentID.String(), *admitted.Subjects[0].AgentID)
}

// A subject admitted against another tenant's issuer would let one organization
// grant a workload access under an issuer it does not own.
func TestWorkloadIdentity_AdmitRefusesForeignIssuer(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const ownerOrg = "org_wi_owner"
	const otherOrg = "org_wi_other"
	workloadIdentityOrg(t, ctx, conn, ownerOrg, "wi-owner")
	otherAgentID := workloadIdentityOrg(t, ctx, conn, otherOrg, "wi-other")

	issuerID, err := testrepo.New(conn).CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: ownerOrg,
		Name:           "owned",
		Issuer:         "https://owner.example/agents",
		JwksUri:        "https://owner.example/agents/jwks.json",
	})
	require.NoError(t, err)

	_, err = svc.AdmitWorkloadSubject(ctx, &gen.AdmitWorkloadSubjectPayload{
		OrganizationID:   otherOrg,
		WorkloadIssuerID: issuerID.String(),
		Subject:          "wimse://owner.example/org/o/agent/a",
		AgentID:          otherAgentID.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Nothing was written for either tenant.
	state, err := svc.GetWorkloadIdentity(ctx, &gen.GetWorkloadIdentityPayload{OrganizationID: ownerOrg})
	require.NoError(t, err)
	require.Empty(t, state.Subjects)
}

func TestWorkloadIdentity_TeardownWithdrawsAdmissionsToo(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_wi_teardown"
	agentID := workloadIdentityOrg(t, ctx, conn, orgID, "wi-teardown")

	created, err := svc.CreateWorkloadIssuer(ctx, &gen.CreateWorkloadIssuerPayload{
		OrganizationID: orgID,
		Name:           "anthropic",
		Issuer:         "https://issuer.example/agents",
		JwksURI:        "https://issuer.example/agents/jwks.json",
	})
	require.NoError(t, err)
	_, err = svc.AdmitWorkloadSubject(ctx, &gen.AdmitWorkloadSubjectPayload{
		OrganizationID:   orgID,
		WorkloadIssuerID: created.Issuers[0].ID,
		Subject:          "wimse://issuer.example/org/o/agent/a",
		AgentID:          agentID.String(),
	})
	require.NoError(t, err)

	torn, err := svc.TeardownWorkloadIssuer(ctx, &gen.TeardownWorkloadIssuerPayload{
		OrganizationID:   orgID,
		WorkloadIssuerID: created.Issuers[0].ID,
	})
	require.NoError(t, err)
	require.Empty(t, torn.Issuers)
	// The point of doing both in one step: a subject left admitted under a
	// withdrawn issuer is invisible in the UI but still a row.
	require.Empty(t, torn.Subjects)
}

func TestWorkloadIdentity_TeardownForeignIssuerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const ownerOrg = "org_wi_td_owner"
	const otherOrg = "org_wi_td_other"
	workloadIdentityOrg(t, ctx, conn, ownerOrg, "wi-td-owner")
	workloadIdentityOrg(t, ctx, conn, otherOrg, "wi-td-other")

	issuerID, err := testrepo.New(conn).CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: ownerOrg,
		Name:           "owned",
		Issuer:         "https://owner.example/agents",
		JwksUri:        "https://owner.example/agents/jwks.json",
	})
	require.NoError(t, err)

	_, err = svc.TeardownWorkloadIssuer(ctx, &gen.TeardownWorkloadIssuerPayload{
		OrganizationID:   otherOrg,
		WorkloadIssuerID: issuerID.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	state, err := svc.GetWorkloadIdentity(ctx, &gen.GetWorkloadIdentityPayload{OrganizationID: ownerOrg})
	require.NoError(t, err)
	require.Len(t, state.Issuers, 1)
}

func TestWorkloadIdentity_AuthenticationHostUnknownIssuerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_wi_host"
	workloadIdentityOrg(t, ctx, conn, orgID, "wi-host")

	_, err := svc.SetWorkloadAuthenticationHost(ctx, &gen.SetWorkloadAuthenticationHostPayload{
		OrganizationID:      orgID,
		UserSessionIssuerID: uuid.New().String(),
		Enabled:             true,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestWorkloadIdentity_RejectsNonUUIDIdentifiers(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_wi_invalid"
	workloadIdentityOrg(t, ctx, conn, orgID, "wi-invalid")

	_, err := svc.AdmitWorkloadSubject(ctx, &gen.AdmitWorkloadSubjectPayload{
		OrganizationID:   orgID,
		WorkloadIssuerID: "not-a-uuid",
		Subject:          "s",
		AgentID:          uuid.New().String(),
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = svc.CreateWorkloadIssuer(ctx, &gen.CreateWorkloadIssuerPayload{
		OrganizationID: orgID,
		ProjectID:      conv.PtrEmpty("not-a-uuid"),
		Name:           "n",
		Issuer:         "https://i.example",
		JwksURI:        "https://i.example/jwks.json",
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
