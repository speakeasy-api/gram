package mcp

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"testing"
)

// Production decision/permission contracts, not SQL or browser coverage.
func gapPathsLive(kind string) upstreamclientsrepo.GetAuthClientSelectionRow {
	id := validUUID(uuid.New())
	return upstreamclientsrepo.GetAuthClientSelectionRow{
		AuthClientID: id, ClientID: id, ClientType: pgtype.Text{String: kind, Valid: true},
		ClientEnabled: pgtype.Bool{Bool: true, Valid: true}, Associated: true,
		RegistrationID: validUUID(uuid.New()), RegistrationIssuerID: validUUID(uuid.New()),
		RegistrationLive: true, RegistrationAttached: true, RequiresUpstreamAuth: true,
	}
}

func TestAuthGapPathsUnmappedEnforcementBoundary(t *testing.T) {
	for _, noAuth := range []bool{false, true} {
		t.Run(map[bool]string{false: "historical_oauth", true: "historical_headers"}[noAuth], func(t *testing.T) {
			row := upstreamclientsrepo.GetAuthClientSelectionRow{NoAuth: noAuth, RequiresUpstreamAuth: true, ServerHasHeaderClients: noAuth}
			selected := decideAuthClient(row)
			require.Equal(t, authClientLegacy, selected.outcome)
			require.Equal(t, uuid.Nil, selected.clientID)
			// resolveEnforcedAuthClient applies this boundary to unnamed choices.
			enforced := withoutLegacyFallback(selected)
			require.Equal(t, authClientGap, enforced.outcome)
			require.Equal(t, authClientReasonUnmapped, enforced.reason)
			require.False(t, enforced.sendsClientHeaders())
		})
	}
	row := upstreamclientsrepo.GetAuthClientSelectionRow{NoAuth: true, ServerHasHeaderClients: true}
	require.Equal(t, authClientNoAuth, withoutLegacyFallback(decideAuthClient(row)).outcome, "explicit no-auth remains usable")
}

func TestAuthGapPathsIncompleteBackfill(t *testing.T) {
	for _, kind := range []string{authClientTypeRemoteSession, authClientTypeStaticCredential, authClientTypePassThrough} {
		t.Run(kind, func(t *testing.T) {
			row := gapPathsLive(kind)
			row.CredentialHeadersUncovered = true
			res := decideAuthClient(row)
			require.Equal(t, authClientGap, res.outcome)
			require.Equal(t, authClientReasonSetup, res.reason)
			require.False(t, res.sendsClientHeaders())
			row.CredentialHeadersUncovered = false
			require.Equal(t, authClientResolved, decideAuthClient(row).outcome)
		})
	}
	row := upstreamclientsrepo.GetAuthClientSelectionRow{NoAuth: true, UpstreamAuthPending: true}
	require.Equal(t, authClientGap, decideAuthClient(row).outcome)
	require.Equal(t, authClientReasonNoAuthStale, decideAuthClient(row).reason)
}

type gapPathsPermit struct {
	allowed  bool
	err      error
	requests []authClientPermitRequest
}

func (p *gapPathsPermit) Permitted(_ context.Context, req authClientPermitRequest) (bool, error) {
	p.requests = append(p.requests, req)
	return p.allowed, p.err
}

func TestAuthGapPathsMappedCompatibilityPermissionGate(t *testing.T) {
	cases := []struct {
		name, kind, reason string
		mutate             func(*upstreamclientsrepo.GetAuthClientSelectionRow)
		sends              bool
	}{
		{"stale_oauth", authClientTypeRemoteSession, authClientReasonHeadersStale, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }, false},
		{"stale_static", authClientTypeStaticCredential, authClientReasonHeadersStale, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }, false},
		{"ema", authClientTypeEMA, authClientReasonEMA, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) {}, false},
		{"may_chain", authClientTypeRemoteSession, authClientReasonLegacyMayChain, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationHasEma = true }, true},
		{"tunneled_static", authClientTypeStaticCredential, authClientReasonTunneledHeaders, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }, false},
		{"tunneled_pass_through", authClientTypePassThrough, authClientReasonTunneledHeaders, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := gapPathsLive(tc.kind)
			tc.mutate(&row)
			selected := decideAuthClient(row)
			require.Equal(t, authClientLegacy, selected.outcome)
			require.Equal(t, tc.reason, selected.reason)
			require.Equal(t, tc.sends, selected.sendsClientHeaders())
			req := authClientPermitRequest{OrganizationID: "org_fixture", ClientID: selected.clientID, ClientType: selected.clientType, RegistrationID: selected.choice.clientID, Subject: urn.NewUserSubject("fixture_user")}
			for _, allowed := range []bool{true, false} {
				p := &gapPathsPermit{allowed: allowed}
				s := &Service{authClientPermitter: p}
				checked, err := s.checkAuthClientPermission(t.Context(), req, selected, nil)
				require.NoError(t, err)
				require.Len(t, p.requests, 1, "mapped compatibility must not bypass mcp:auth")
				require.Equal(t, row.AuthClientID.UUID, p.requests[0].ClientID)
				if tc.kind == authClientTypeRemoteSession {
					require.Equal(t, row.RegistrationID, p.requests[0].RegistrationID)
					require.Equal(t, "fixture_user", p.requests[0].ConnectionOwner)
				}
				if allowed {
					require.Equal(t, authClientLegacy, checked.outcome)
				} else {
					require.Equal(t, authClientNoAccess, checked.outcome)
					require.Equal(t, authClientReasonNotPermitted, checked.reason)
					require.False(t, checked.sendsClientHeaders())
				}
			}
			failure := errors.New("permission unavailable")
			s := &Service{authClientPermitter: &gapPathsPermit{err: failure}}
			_, err := s.checkAuthClientPermission(t.Context(), req, selected, nil)
			require.ErrorIs(t, err, failure, "permission failure must not become successful fallback")
		})
	}
}

// TestAuthGapPathsDesiredTypedServingBoundary states the requested stricter
// contract separately from today's permission-gated compatibility behavior.
// These are deliberately red boundary tests, not end-to-end serving tests.
// resolveEnforcedAuthClient calls withoutLegacyFallback ONLY for unnamed
// clients. Applying that helper unconditionally here would hide the mapped
// handoff and falsely report typed enforcement coverage.
func TestAuthGapPathsDesiredTypedServingBoundary(t *testing.T) {
	cases := []struct {
		name, kind string
		mutate     func(*upstreamclientsrepo.GetAuthClientSelectionRow)
	}{
		{"G22_EMA", authClientTypeEMA, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) {}},
		{"G22_may_chain", authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.RegistrationHasEma = true }},
		{"G23_tunneled_static", authClientTypeStaticCredential, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }},
		{"G23_tunneled_pass_through", authClientTypePassThrough, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.Tunneled = true }},
		{"G24_stale_oauth", authClientTypeRemoteSession, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }},
		{"G24_stale_static", authClientTypeStaticCredential, func(r *upstreamclientsrepo.GetAuthClientSelectionRow) { r.HeadersStale = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := gapPathsLive(tc.kind)
			tc.mutate(&row)
			selected := decideAuthClient(row)
			// The explicit no-fallback primitive is fail-closed even for these inputs,
			// but the enforced orchestrator does not invoke it for named clients.
			closed := withoutLegacyFallback(selected)
			require.Equal(t, authClientGap, closed.outcome)
			require.False(t, closed.sendsClientHeaders())
			p := &gapPathsPermit{allowed: true}
			s := &Service{authClientPermitter: p}
			checked, err := s.checkAuthClientPermission(t.Context(), authClientPermitRequest{
				OrganizationID: "org_fixture", ClientID: selected.clientID, ClientType: selected.clientType,
				RegistrationID: selected.choice.clientID, Subject: urn.NewUserSubject("fixture_user"),
			}, selected, nil)
			require.NoError(t, err)
			require.Len(t, p.requests, 1)
			require.NotEqual(t, uuid.Nil, checked.clientID)
			// Desired: a permitted mapped choice is typed or fails closed, not handed
			// back to legacy. No policy is invented for which outcome replaces it.
			require.Contains(t, []authClientOutcome{authClientResolved, authClientGap, authClientNoAccess}, checked.outcome,
				"requested typed-serving boundary is unsupported: reason=%s; current enforcement retains mapped compatibility", checked.reason)
		})
	}
}

// Backend shadow characterization only, not parity proof: agreement proves
// neither usable headers nor permission. This is not typed browser evidence.
func TestAuthGapPathsShadowAgreementDoesNotAuthorize(t *testing.T) {
	selected := decideAuthClient(gapPathsLive(authClientTypeStaticCredential))
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	logger := testenv.NewLogger(t)
	p := &gapPathsPermit{allowed: false}
	s := &Service{logger: logger, metrics: mcpmetrics.NewMetrics(provider.Meter("gap-paths"), logger), authClientPermitter: p}
	s.reportAuthClientParity(t.Context(), "org_fixture", uuid.New(), selected, upstreamChoice{}, nil)
	require.Equal(t, map[attribute.Set]int64{parityKey(true, authClientResolved, authClientNoAuth, authClientReasonNone): 1}, parityPoints(t, reader))
	require.Empty(t, p.requests, "comparison cannot stand in for permission enforcement")
	checked, err := s.checkAuthClientPermission(t.Context(), authClientPermitRequest{ClientID: selected.clientID, ClientType: selected.clientType}, selected, nil)
	require.NoError(t, err)
	require.Equal(t, authClientNoAccess, checked.outcome)
	require.Len(t, p.requests, 1)
	missing := applyAuthClientHeaders(selected, nil)
	require.Equal(t, authClientGap, missing.outcome)
	require.Equal(t, authClientReasonSetup, missing.reason)
	supplied := applyAuthClientHeaders(selected, []proxy.ConfiguredHeader{{Name: "X-Fixture-Key", StaticValue: "fixture"}})
	require.Equal(t, authClientResolved, supplied.outcome)
}
