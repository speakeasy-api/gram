package risk_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/authority"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/catalog"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	mcpapprovalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// intakeTokenInput names the parts of a bypass token a binding test varies.
type intakeTokenInput struct {
	organizationID string
	projectID      string
	requesterID    string
	policyID       string
	fullURL        string
}

func intakeToken(t *testing.T, ti *testInstance, input intakeTokenInput) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken(t.Context(), ti.cacheAdapter, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         input.organizationID,
		ProjectID:              input.projectID,
		RequesterUserID:        input.requesterID,
		ObservedName:           nil,
		ObservedFullURL:        &input.fullURL,
		ObservedURLHost:        nil,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           input.policyID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)
	return token
}

func redeem(ctx context.Context, ti *testInstance, token string) (*gen.PolicyBypassRedemption, error) {
	//nolint:wrapcheck // tests assert on the service error exactly as returned
	return ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		SessionToken: nil,
		RequestToken: token,
	})
}

func requireRedemptionOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

// A leaked link cannot be cashed in from another organization's session: the
// token's org binding is checked before anything else trusts its ids.
func TestCreatePolicyBypassRequest_OrganizationMismatchIsForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Org Binding"),
	})
	require.NoError(t, err)

	token := intakeToken(t, ti, intakeTokenInput{
		organizationID: "org-somebody-else",
		projectID:      authCtx.ProjectID.String(),
		requesterID:    authCtx.UserID,
		policyID:       policy.ID,
		fullURL:        "https://mcp.example.com/org-binding",
	})

	_, err = redeem(ctx, ti, token)
	requireRedemptionOopsCode(t, err, oops.CodeForbidden)
}

// A token bound to a known requester redeems only for that requester.
func TestCreatePolicyBypassRequest_RequesterMismatchIsForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Requester Binding"),
	})
	require.NoError(t, err)

	token := intakeToken(t, ti, intakeTokenInput{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
		requesterID:    "someone-else-entirely",
		policyID:       policy.ID,
		fullURL:        "https://mcp.example.com/requester-binding",
	})

	_, err = redeem(ctx, ti, token)
	requireRedemptionOopsCode(t, err, oops.CodeForbidden)
}

// A token whose project has since been deleted (or never belonged to the
// caller's organization) fails as a stale link before the intake is asked to
// open an approval request for it.
func TestCreatePolicyBypassRequest_StaleProjectIsNotFoundBeforeIntake(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err: nil, requestID: "", status: "",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Stale Project"),
	})
	require.NoError(t, err)

	token := intakeToken(t, ti, intakeTokenInput{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      uuid.NewString(),
		requesterID:    authCtx.UserID,
		policyID:       policy.ID,
		fullURL:        "https://mcp.example.com/stale-project",
	})

	_, err = redeem(ctx, ti, token)
	requireRedemptionOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, intake.gotServerURL, "no approval request may be opened for a stale link")
}

// A token whose policy has since been deleted fails the same way.
func TestCreatePolicyBypassRequest_StalePolicyIsNotFoundBeforeIntake(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err: nil, requestID: "", status: "",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	token := intakeToken(t, ti, intakeTokenInput{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
		requesterID:    authCtx.UserID,
		policyID:       uuid.NewString(),
		fullURL:        "https://mcp.example.com/stale-policy",
	})

	_, err := redeem(ctx, ti, token)
	requireRedemptionOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, intake.gotServerURL, "no approval request may be opened for a stale link")
}

// Only a Forbidden intake error means "feature not enabled, use the legacy
// flow". Anything else is a real failure and must surface as one — silently
// minting a legacy bypass row would hide it.
func TestCreatePolicyBypassRequest_IntakeFailureDoesNotFallBack(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err:       oops.E(oops.CodeBadRequest, nil, "target is not a valid server URL"),
		requestID: "", status: "",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Intake Failure"),
	})
	require.NoError(t, err)

	_, err = redeem(ctx, ti, intakeToken(t, ti, intakeTokenInput{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
		requesterID:    authCtx.UserID,
		policyID:       policy.ID,
		fullURL:        "https://mcp.example.com/intake-failure",
	}))
	requireRedemptionOopsCode(t, err, oops.CodeUnexpected)

	// No legacy bypass row was written on the failure path.
	list, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil, PolicyID: &policy.ID, Status: nil,
	})
	require.NoError(t, err)
	require.Empty(t, list.Requests)
}

// A blocked server on a non-http(s) scheme cannot enter the approval
// workflow — the intake would reject it — so its link keeps redeeming into
// the legacy bypass request instead of failing.
func TestCreatePolicyBypassRequest_NonHTTPURLKeepsLegacyFlow(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err: nil, requestID: "should-not-be-used", status: "requested",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Websocket Server"),
	})
	require.NoError(t, err)

	redemption, err := redeem(ctx, ti, intakeToken(t, ti, intakeTokenInput{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
		requesterID:    authCtx.UserID,
		policyID:       policy.ID,
		fullURL:        "ws://mcp.example.com/socket",
	}))
	require.NoError(t, err)
	require.Equal(t, "bypass_request", redemption.Kind)
	require.Equal(t, "requested", redemption.Status)
	require.Empty(t, intake.gotServerURL, "the intake must not see a URL it cannot admit")
	require.Equal(t, authCtx.UserID, redeemedBypassRow(t, ctx, ti, redemption).RequesterUserID)
}

// riskIntakeQuietProbes and riskIntakeNotFoundRegistry mirror the mcpapproval
// package's own test stubs, so the real service can be wired as the intake
// without reaching real registries or MCP servers.
type riskIntakeQuietProbes struct{}

func (riskIntakeQuietProbes) DiscoverAuthority(_ context.Context, _ string) (*authority.Declaration, error) {
	return nil, nil
}

func (riskIntakeQuietProbes) ListToolDeclarations(_ context.Context, _ string) ([]capability.Declaration, error) {
	return nil, nil
}

func (riskIntakeQuietProbes) Lookup(_ context.Context, _ string, _ bool) (*catalog.Match, error) {
	return nil, nil
}

type riskIntakeNotFoundRegistry struct{}

func (riskIntakeNotFoundRegistry) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		Status:     http.StatusText(http.StatusNotFound),
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{"error":"Not found"}`)),
		Header:     http.Header{},
		Request:    request,
	}, nil
}

// riskIntakeEmptyAdvisoryDB answers every advisory query with an empty
// document, OSV's shape for a package it has nothing on.
type riskIntakeEmptyAdvisoryDB struct{}

func (riskIntakeEmptyAdvisoryDB) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		Status:     http.StatusText(http.StatusOK),
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     http.Header{},
		Request:    request,
	}, nil
}

// The block link redeems end to end through the REAL mcpapproval service —
// wired the way the server wires it — and lands as an approval request with
// the blocked employee attached, deduplicated on the canonical URL, with no
// legacy bypass row minted.
func TestCreatePolicyBypassRequest_RealIntakeOpensApprovalRequest(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		logger := testenv.NewLogger(t)
		tracerProvider := testenv.NewTracerProvider(t)
		authzEngine := authz.NewEngine(logger, instance.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
		assembler := evidence.NewAssembler(
			packagemeta.NewClient(riskIntakeNotFoundRegistry{}),
			repometa.NewClient(riskIntakeNotFoundRegistry{}),
			advisories.NewClient(riskIntakeEmptyAdvisoryDB{}),
			domainmeta.NewClient(riskIntakeNotFoundRegistry{}),
			telemetryrepo.New(instance.chConn),
			riskIntakeQuietProbes{},
			riskIntakeQuietProbes{},
			riskIntakeQuietProbes{},
		)

		instance.approvalIntake = mcpapproval.NewService(logger, tracerProvider, instance.conn, instance.sessionManager, authzEngine, flags, audit.NewLogger(), assembler)
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	flags.SetFlag(feature.FlagMCPApproval, authCtx.ActiveOrganizationID, true)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Real Intake"),
	})
	require.NoError(t, err)

	redemption, err := redeem(ctx, ti, intakeToken(t, ti, intakeTokenInput{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
		requesterID:    authCtx.UserID,
		policyID:       policy.ID,
		fullURL:        "https://MCP.Example.com:443/real-intake?session=secret",
	}))
	require.NoError(t, err)
	require.Equal(t, "approval_request", redemption.Kind)
	require.Equal(t, "requested", redemption.Status)

	// The redemption opened a real review: canonical dedupe key, redacted
	// stored reference, the blocked employee attached as the requester.
	row, err := mcpapprovalrepo.New(ti.conn).GetApprovalRequest(ctx, mcpapprovalrepo.GetApprovalRequestParams{
		ID:        uuid.MustParse(redemption.ID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com/real-intake", row.TargetKey)
	require.NotContains(t, row.TargetRaw, "secret")
	require.Equal(t, "requested", row.Status)

	requesters, err := mcpapprovalrepo.New(ti.conn).ListRequestersForApprovalRequest(ctx, mcpapprovalrepo.ListRequestersForApprovalRequestParams{
		McpApprovalRequestID: uuid.MustParse(redemption.ID),
		ProjectID:            *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, requesters, 1)
	require.Equal(t, authCtx.UserID, requesters[0].UserID)

	// No legacy bypass row rode along.
	list, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil, PolicyID: &policy.ID, Status: nil,
	})
	require.NoError(t, err)
	require.Empty(t, list.Requests)
}
