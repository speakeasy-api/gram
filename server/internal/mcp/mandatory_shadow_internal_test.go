package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const shadowOrg = "org_shadow"

func sessionContext(t *testing.T, orgID string, grants ...authz.Grant) context.Context {
	t.Helper()
	session := "session_1"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID, UserID: "user_1", SessionID: &session, AccountType: "enterprise",
	})
	return authz.GrantsToContext(ctx, grants)
}

type shadowHarness struct {
	service *Service
	reader  *sdkmetric.ManualReader
	logs    *bytes.Buffer
}

func newShadowHarness(t *testing.T, enabled bool) shadowHarness {
	t.Helper()
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagMandatoryAuthzShadow, shadowOrg, enabled)
	reader := sdkmetric.NewManualReader()
	var logs bytes.Buffer
	return shadowHarness{
		service: &Service{
			logger:             slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
			metrics:            mcpmetrics.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t)),
			features:           flags,
			mandatoryShadowLog: newMandatoryShadowLog(),
			authz:              authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient()),
		},
		reader: reader,
		logs:   &logs,
	}
}

func (h shadowHarness) points(t *testing.T) map[attribute.Set]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(t.Context(), &rm))
	points := map[attribute.Set]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "mcp.authz.mandatory_shadow" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] += dp.Value
			}
		}
	}
	return points
}

func (h shadowHarness) logLines(level string) int {
	return strings.Count(h.logs.String(), `"level":"`+level+`"`)
}

func shadowKey(surface, caller, principal string, legacy bool, outcome, source, reason string) attribute.Set {
	return attribute.NewSet(
		attribute.String("gram.org.id", shadowOrg),
		attribute.String("gram.authz.mandatory.surface", surface),
		attribute.String("gram.authz.mandatory.caller", caller),
		attribute.String("gram.authz.mandatory.principal", principal),
		attribute.Bool("gram.authz.mandatory.legacy_allowed", legacy),
		attribute.Bool("gram.authz.mandatory.legacy_auth_ceiling", false),
		attribute.String("gram.authz.mandatory.outcome", outcome),
		attribute.String("gram.authz.mandatory.source", source),
		attribute.String("gram.authz.mandatory.reason", reason),
	)
}

func TestMandatoryShadowCallerClass(t *testing.T) {
	t.Parallel()

	h := newShadowHarness(t, true)
	assistant := contextvalues.SetAssistantPrincipal(sessionContext(t, shadowOrg), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	for name, tc := range map[string]struct {
		ctx    func() context.Context
		caller string
	}{
		"anonymous":               {t.Context, mandatoryCallerAnonymous},
		"another organization":    {func() context.Context { return sessionContext(t, "org_other") }, mandatoryCallerCrossOrg},
		"signed-in user":          {func() context.Context { return sessionContext(t, shadowOrg) }, mandatoryCallerRepresented},
		"assistant owner is attr": {func() context.Context { return assistant }, mandatoryCallerUnrepresented},
	} {
		shadow := h.service.newMandatoryShadow(tc.ctx(), shadowOrg)
		require.NotNil(t, shadow, name)
		require.NoError(t, shadow.err, name)
		require.Equal(t, tc.caller, shadow.callerClass(), name)
	}
}

func TestShadowMandatoryRecordsAgreementWithoutChangingTheRequest(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	allowed := sessionContext(t, shadowOrg, authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource))

	h := newShadowHarness(t, true)
	h.service.shadowMandatory(allowed, mandatorySurfaceProxiedConnect, shadowOrg, true, connect)
	h.service.shadowMandatory(allowed, mandatorySurfaceProxiedConnect, shadowOrg, false, connect)
	h.service.shadowMandatory(sessionContext(t, shadowOrg), mandatorySurfaceHostedConnect, shadowOrg, true, connect)
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceProxiedConnect, mandatoryCallerRepresented, "user", true, "allow", "request", "none"):    1,
		shadowKey(mandatorySurfaceProxiedConnect, mandatoryCallerRepresented, "user", false, "allow", "request", "none"):   1,
		shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerRepresented, "user", true, "deny", "request", "no_policy"): 1,
	}, h.points(t))
	require.Equal(t, 2, h.logLines("INFO"), "each disagreement logs")
	require.Equal(t, 1, h.logLines("DEBUG"), "agreements log at debug")
	require.Contains(t, h.logs.String(), `"gram.authz.mandatory.principal_id":"user_1"`)

	off := newShadowHarness(t, false)
	require.Nil(t, off.service.newMandatoryShadow(allowed, shadowOrg))
	off.service.shadowMandatory(allowed, mandatorySurfaceProxiedConnect, shadowOrg, true, connect)
	require.Empty(t, off.points(t), "the flag gates every evaluation")
	require.Empty(t, off.logs.String())
}

func TestShadowMandatoryAnonymousAndCrossOrgCallers(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	h := newShadowHarness(t, true)
	h.service.shadowMandatory(t.Context(), mandatorySurfaceProxiedConnect, shadowOrg, true, connect)
	foreign := sessionContext(t, "org_other", authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource))
	h.service.shadowMandatory(foreign, mandatorySurfaceProxiedConnect, shadowOrg, true, connect)
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceProxiedConnect, mandatoryCallerAnonymous, "anonymous", true, "deny", "none", "unauthenticated"):    1,
		shadowKey(mandatorySurfaceProxiedConnect, mandatoryCallerCrossOrg, "user", true, "deny", "request", "organization_mismatch"): 1,
	}, h.points(t), "another organization's grants are never authority here")
}

func TestShadowMandatoryAuthClientChecksTheStoredOwner(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	client := uuid.New()
	shared := authz.NewGrantWithSelector(authz.ScopeMCPAuth, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: client.String()})
	personal := authz.NewGrantWithSelector(authz.ScopeMCPAuth, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource,
		authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession, authz.SelectorKeyConnectionOwner: authz.SelectorValueRepresentedUser,
	})

	h := newShadowHarness(t, true)
	record := func(ctx context.Context, clientType string, subject urn.SessionSubject) {
		h.service.shadowMandatory(ctx, mandatorySurfaceAuthClient, shadowOrg, true, mandatoryAuthClientCheck(project, client, clientType, subject))
	}
	record(sessionContext(t, shadowOrg, shared), authClientTypeServiceAccount, urn.NewUserSubject("user_1"))
	record(sessionContext(t, shadowOrg, personal), authClientTypeRemoteSession, urn.NewUserSubject("user_1"))
	record(sessionContext(t, shadowOrg, personal), authClientTypeRemoteSession, urn.NewUserSubject("user_2"))
	record(sessionContext(t, shadowOrg), authClientTypeServiceAccount, urn.NewUserSubject("user_1"))
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceAuthClient, mandatoryCallerRepresented, "user", true, "allow", "request", "none"):       2,
		shadowKey(mandatorySurfaceAuthClient, mandatoryCallerRepresented, "user", true, "deny", "request", "not_granted"): 1,
		shadowKey(mandatorySurfaceAuthClient, mandatoryCallerRepresented, "user", true, "deny", "request", "no_policy"):   1,
	}, h.points(t), "a personal connection only serves its own owner")
}

func TestShadowRequireOnlyComparesDecisions(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	ctx := sessionContext(t, shadowOrg)
	h := newShadowHarness(t, true)
	shadow := h.service.newMandatoryShadow(ctx, shadowOrg)
	shadow.shadowRequire(ctx, mandatorySurfaceHostedConnect, errors.New("database unavailable"), connect)
	require.Empty(t, h.points(t), "a failed evaluation is not a legacy deny")

	shadow.shadowRequire(ctx, mandatorySurfaceHostedConnect, oops.C(oops.CodeForbidden), connect)
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerRepresented, "user", false, "deny", "request", "no_policy"): 1,
	}, h.points(t))
}

func TestShadowMandatoryDedupesDisagreementLogs(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	h := newShadowHarness(t, true)
	for range 3 {
		h.service.shadowMandatory(sessionContext(t, shadowOrg), mandatorySurfaceHostedConnect, shadowOrg, true, connect)
	}
	require.Equal(t, 1, h.logLines("INFO"))
	require.Equal(t, 2, h.logLines("DEBUG"), "repeats drop to debug")
	require.Equal(t, int64(3), h.points(t)[shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerRepresented, "user", true, "deny", "request", "no_policy")])
}

func TestShadowMandatoryCountsEvaluationFailures(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	// An API key with no authorization mode is a broken credential, not a decision.
	broken := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: shadowOrg, APIKeyID: uuid.NewString()})

	h := newShadowHarness(t, true)
	h.service.shadowMandatory(broken, mandatorySurfaceHostedConnect, shadowOrg, true, connect)
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerUnrepresented, "api_key", true, "error", "none", "none"): 1,
	}, h.points(t))
	require.Contains(t, h.logs.String(), "mandatory authz shadow failed")

	canceled, cancel := context.WithCancel(broken)
	cancel()
	h.service.shadowMandatory(canceled, mandatorySurfaceHostedConnect, shadowOrg, true, connect)
	require.Equal(t, int64(1), h.points(t)[shadowKey(mandatorySurfaceHostedConnect, mandatoryCallerUnrepresented, "api_key", true, "error", "none", "none")], "a client that went away records nothing")
}

func TestShadowMandatoryRecordsSkippedChecks(t *testing.T) {
	t.Parallel()

	h := newShadowHarness(t, true)
	h.service.newMandatoryShadow(sessionContext(t, shadowOrg), shadowOrg).recordSkipped(t.Context(), mandatorySurfaceAuthClient, true, mandatoryReasonResolverOff, "server_1", "project_1")
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceAuthClient, mandatoryCallerRepresented, "user", true, "skipped", "none", mandatoryReasonResolverOff): 1,
	}, h.points(t), "a check that was not evaluated is never an allow")
}

func TestAdmitMetaMembersShadowLeavesAdmissionUnchanged(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	candidate := func(visibility string) metaMemberCandidate {
		id := uuid.New()
		return metaMemberCandidate{
			serverID: id, projectID: project, projectSlug: "p", slug: "s-" + id.String()[:8], name: "s", sortOrder: 0,
			visibility: visibility, unproxied: false, toolsetID: uuid.NullUUID{}, remoteServerID: uuid.NullUUID{UUID: id, Valid: true},
			tunneledServerID: uuid.NullUUID{}, environmentID: uuid.NullUUID{}, toolVariationsGroupID: uuid.NullUUID{},
			remoteSessionIssuerID: uuid.NullUUID{}, tunneledResourceIdentifier: "",
		}
	}
	public, granted, denied := candidate("public"), candidate("private"), candidate("private")
	ctx := sessionContext(t, shadowOrg, authz.NewGrant(authz.ScopeMCPConnect, granted.serverID.String()))

	admitted := func(h shadowHarness) []uuid.UUID {
		_, members, err := h.service.admitMetaMembers(ctx, testenv.NewLogger(t), mandatorySurfaceConsent, shadowOrg, []metaMemberCandidate{public, granted, denied})
		require.NoError(t, err)
		ids := make([]uuid.UUID, 0, len(members))
		for _, member := range members {
			ids = append(ids, member.serverID)
		}
		return ids
	}

	on, off := newShadowHarness(t, true), newShadowHarness(t, false)
	require.Equal(t, admitted(off), admitted(on), "the shadow never changes which members are admitted")
	require.Equal(t, []uuid.UUID{public.serverID, granted.serverID}, admitted(off))
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceConsent, mandatoryCallerRepresented, "user", true, "deny", "request", "not_granted"):  1,
		shadowKey(mandatorySurfaceConsent, mandatoryCallerRepresented, "user", true, "allow", "request", "none"):        1,
		shadowKey(mandatorySurfaceConsent, mandatoryCallerRepresented, "user", false, "deny", "request", "not_granted"): 1,
	}, on.points(t), "each member is recorded once under the caller's surface")
}

func TestShadowMandatoryDedupeHoldsUnderConcurrency(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	h := newShadowHarness(t, true)
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			h.service.shadowMandatory(sessionContext(t, shadowOrg), mandatorySurfaceHostedConnect, shadowOrg, true, connect)
		})
	}
	wg.Wait()
	require.Equal(t, 1, h.logLines("INFO"))
}

func TestShadowAuthClientUseSkipsPersonalClientsWithoutAUser(t *testing.T) {
	t.Parallel()

	h := newShadowHarness(t, true)
	project, client := uuid.New(), uuid.New()
	h.service.shadowAuthClientUse(sessionContext(t, shadowOrg), shadowOrg, true, project, client, authClientTypeRemoteSession, urn.NewAgentSubject(uuid.New()))
	h.service.shadowAuthClientUse(sessionContext(t, shadowOrg), shadowOrg, false, project, client, authClientTypeRemoteSession, urn.NewAgentSubject(uuid.New()))
	require.Equal(t, map[attribute.Set]int64{
		shadowKey(mandatorySurfaceAuthClient, mandatoryCallerRepresented, "user", true, "skipped", "none", mandatoryReasonNoConnectionOwner):  1,
		shadowKey(mandatorySurfaceAuthClient, mandatoryCallerRepresented, "user", false, "skipped", "none", mandatoryReasonNoConnectionOwner): 1,
	}, h.points(t), "a personal client used without a user to own it is not an invalid check, and keeps today's decision")
}

func TestShadowMandatoryNeverRecordsCanceledRequests(t *testing.T) {
	t.Parallel()

	connect := authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1")
	h := newShadowHarness(t, true)
	ctx, cancel := context.WithCancel(sessionContext(t, shadowOrg))
	shadow := h.service.newMandatoryShadow(ctx, shadowOrg)
	cancel()
	shadow.record(ctx, mandatorySurfaceHostedConnect, true, connect)
	require.Empty(t, h.points(t))
}

func TestWithMandatoryAdmissionIsIdempotentForOneCaller(t *testing.T) {
	t.Parallel()

	ctx := withMandatoryAdmission(sessionContext(t, shadowOrg))
	require.Equal(t, ctx, withMandatoryAdmission(ctx), "the same caller keeps its one admission")
	other := withMandatoryAdmission(sessionContext(t, shadowOrg))
	require.NotSame(t, ctx.Value(mandatoryAdmissionKey{}), other.Value(mandatoryAdmissionKey{}))
}

func TestShadowMandatoryLabelsLegacyAuthCeilings(t *testing.T) {
	t.Parallel()

	for _, legacy := range []bool{true, false} {
		h := newShadowHarness(t, true)
		connect := authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource)
		h.service.authz = authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
			AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
				return authz.PrincipalCredentialAdmission{
					OwnerUserID: "user_owner", Credential: []authz.Grant{connect}, Agent: []authz.Grant{connect}, Owner: []authz.Grant{connect},
					Authorizer: authz.RepresentedUserPolicy{UserID: "user_owner", Policy: []authz.Grant{connect}}, LegacyAuthCeiling: legacy,
				}, nil
			},
		})
		ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: shadowOrg},
			urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()), contextvalues.PrincipalCredential{AuthorizerUserID: "user_owner"})
		h.service.shadowMandatory(ctx, mandatorySurfaceProxiedConnect, shadowOrg, true, authz.MCPCheck(authz.ScopeMCPConnect, "server_1", "project_1"))

		points := h.points(t)
		require.Len(t, points, 1)
		for set := range points {
			value, ok := set.Value("gram.authz.mandatory.legacy_auth_ceiling")
			require.True(t, ok)
			require.Equal(t, legacy, value.AsBool())
			principal, _ := set.Value("gram.authz.mandatory.principal")
			require.Equal(t, "agent", principal.AsString())
		}
		require.Contains(t, h.logs.String(), fmt.Sprintf(`"gram.authz.mandatory.legacy_auth_ceiling":%t`, legacy))
	}
}
