package mcp_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// A configured gateway refuses workload assertions it cannot serve, and each
// refusal is recorded once under the gateway, through the real token handler,
// raw row, rollup, and health reader. A subject the trusted issuer signed but
// the organization never admitted is a policy block; an admitted one is still
// refused, because a gateway cannot carry an agent session. Both answer the
// same generic invalid_grant. Neither names the caller or client, since
// neither was validated, and neither the presented subject nor the assertion
// reaches the recorded attempts or the health read.
func TestGatewayInboundWorkloadAssertionRefusals(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	writes := watchConnectionAttempts(t, f.ti)
	projectID := f.fx.target.ProjectID
	slug := "inbound-workload-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, f.ctx, f.ti.conn, projectID, f.fx.orgID, slug, f.fx.target.UserSessionIssuerID)
	other := createMetaMcpEndpoint(t, f.ctx, f.ti.conn, projectID, f.fx.orgID, "inbound-workload-other-"+uuid.NewString(), f.fx.target.UserSessionIssuerID)
	gatewayIssuer, _ := fetchAdvertisedIssuer(t, f.ctx, f.ti, slug)
	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(f.ctx, f.ti.logger, slug, "mcp")
	require.NoError(t, err)
	resource, err := endpoint.RootURL(f.ti.serverURL.String())
	require.NoError(t, err)

	const outsider = "repo:example-outsider/app:ref:refs/heads/main"
	var secrets []string
	exchange := func(subject string) {
		t.Helper()
		assertion := oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", oauthtest.WorkloadClaims(f.issuer, subject, gatewayIssuer))
		secrets = append(secrets, assertion)
		secrets = append(secrets, strings.Split(assertion, ".")...)
		form := url.Values{}
		form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeJWTBearer)
		form.Set(oauthwire.ParamAssertion, assertion)
		form.Set(oauthwire.ParamResource, resource)
		// The same invalid_grant for both, naming no policy.
		requireWorkloadGrantRefused(t, postForm(t, f.ti, slug, "token", form))
		require.Equal(t, 1, writes.written(t), "one attempt per refused request")
	}
	exchange(outsider)
	exchange(f.subject)

	rows := inboundAttempts(t, projectID, gateway.ID)
	require.Len(t, rows, 2)
	got := make([][]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, []string{row.Stage, row.Outcome, row.FailureClass, row.Reason, row.Subject, row.ClientKey, row.GrantType})
	}
	require.Equal(t, [][]string{
		{"inbound_token", "policy_blocked", "configuration", "subject_not_admitted", "", "unverified_client", oauthwire.GrantTypeJWTBearer},
		{"inbound_token", "failed", "configuration", "endpoint_not_agent_addressable", "", "unverified_client", oauthwire.GrantTypeJWTBearer},
	}, got)
	requireNoSecrets(t, rows, append(secrets, outsider, f.subject, "example-outsider", "payments-api", f.issuerID.String())...)
	require.Empty(t, inboundAttempts(t, projectID, other.ID), "another gateway in the project records nothing")
	require.Empty(t, f.workloadSessions(t), "no session is minted for either refusal")
	require.Equal(t, map[string]uint64{"inbound_token/policy_blocked": 1, "inbound_token/failed": 1},
		stageOutcomes(t, projectID, gateway.ID, "", "unverified_client"), "the rollup counts both under the unknown caller")

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	reader := telemetry.NewConnectionHealth(f.ti.conn, chConn, func(context.Context, string) (bool, error) { return true, nil }, f.ti.authzEngine)
	grants := []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, projectID.String()), authz.NewGrant(authz.ScopeLogRead, projectID.String()), authz.NewGrant(authz.ScopeMCPRead, gateway.ID.String())}
	readCtx := authztest.WithExactGrants(t, f.ctx, grants...)
	target := telemetry.ConnectionHealthTarget{ProjectID: projectID, OrganizationID: f.fx.orgID, GatewayID: gateway.ID}
	window, err := telemetry.ParseConnectionHealthWindow("", "", time.Now().Add(time.Minute))
	require.NoError(t, err)

	summary, err := reader.Summary(readCtx, target, window, telemetry.ConnectionHealthScope{Key: nil, Caller: nil})
	require.NoError(t, err)
	counts := map[string]uint64{}
	for _, stage := range summary.Stages {
		require.Equal(t, stage.Stage == telemetry.ConnectionStageInboundToken, stage.Attempts > 0, stage.Stage)
		for _, outcome := range stage.Outcomes {
			counts[stage.Stage+"/"+outcome.Outcome] = outcome.Count
		}
		if stage.Stage == telemetry.ConnectionStageInboundToken {
			require.EqualValues(t, 2, stage.Attempts, "the policy block stays in the denominator")
			require.True(t, stage.LastCodeIssued.IsZero(), "no sign-in token was written")
		}
	}
	require.Equal(t, map[string]uint64{"inbound_token/policy_blocked": 1, "inbound_token/failed": 1}, counts)

	ranked, err := reader.Hotspots(readCtx, target, window, telemetry.ConnectionHotspotQuery{Stage: telemetry.ConnectionStageInboundToken, ByRate: false, MinAttempts: 1, Limit: 25, Caller: nil})
	require.NoError(t, err)
	require.Len(t, ranked.Hotspots, 1, "both refusals share the unknown caller's key")
	hotspot := ranked.Hotspots[0]
	require.Equal(t, []uint64{2, 1, 1}, []uint64{hotspot.Attempts, hotspot.Failed, hotspot.Blocked})
	require.Equal(t, []string{"anonymous", "", "", "unverified", "unverified_client"},
		[]string{hotspot.Subject.Kind, hotspot.Subject.ID, hotspot.Subject.Key, hotspot.Client.Kind, hotspot.Client.Key})
	reasons := map[string]uint64{}
	for _, reason := range hotspot.Reasons {
		reasons[reason.Reason] = reason.Count
	}
	require.Equal(t, map[string]uint64{"subject_not_admitted": 1, "endpoint_not_agent_addressable": 1}, reasons)

	// The reader keeps its own boundaries: another gateway in the project has
	// nothing, and a gateway the reader may not see is not found.
	otherCtx := authztest.WithExactGrants(t, f.ctx, append(grants[:2:2], authz.NewGrant(authz.ScopeMCPRead, other.ID.String()))...)
	otherSummary, err := reader.Summary(otherCtx, telemetry.ConnectionHealthTarget{ProjectID: projectID, OrganizationID: f.fx.orgID, GatewayID: other.ID}, window, telemetry.ConnectionHealthScope{Key: nil, Caller: nil})
	require.NoError(t, err)
	for _, stage := range otherSummary.Stages {
		require.Zero(t, stage.Attempts, stage.Stage)
	}
	_, err = reader.Summary(otherCtx, target, window, telemetry.ConnectionHealthScope{Key: nil, Caller: nil})
	require.ErrorIs(t, err, telemetry.ErrConnectionHealthGatewayNotFound)
}
