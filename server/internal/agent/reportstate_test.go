package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentserver "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// stateReport is a minimal valid report; tests adjust the fields they cover.
func stateReport(email, serial string) *gen.ReportStatePayload {
	return &gen.ReportStatePayload{
		SchemaVersion:      1,
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		Agent:              &gen.AgentStateVersions{Speakeasyd: "0.42.0", Speakeasy: nil, SpeakeasyUI: nil, Os: new("darwin"), Arch: new("arm64")},
		EnforcementHalted:  false,
		Policy:             &gen.AgentStatePolicy{AppliedEtag: new("etag-1"), AppliedAt: nil, WithheldPlugins: []*gen.AgentStateWithheldPlugin{}, RemoteConfigEtag: nil, RemoteConfigFailed: nil},
		Tools:              []*gen.AgentStateTool{},
		DriftCountingSince: nil,
		Update:             nil,
		Email:              new(email),
		SerialNumber:       new(serial),
		Hostname:           new("devbox"),
		Environment:        nil,
	}
}

func stateReports(t *testing.T, ti *testInstance) []testrepo.DeviceAgentStateReport {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListDeviceAgentStateReportsFixture(t.Context(), ti.orgID)
	require.NoError(t, err)
	return rows
}

// A newer agent can add fields and enum values this server does not know.
// Neither may fail the report.
func TestReportState_HTTPAcceptsUnknownFieldsAndValues(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/agent.reportState", bytes.NewBufferString(`{
		"schema_version": 2,
		"generated_at": "2026-10-26T12:00:00Z",
		"agent": {"speakeasyd": "0.42.0", "future_binary": "1.0.0"},
		"enforcement_halted": false,
		"policy": {"withheld_plugins": [], "future_policy_field": true},
		"tools": [{"tool": "claude_code", "layer": "kernel", "outcome": "quarantined"}],
		"future_top_level": {"anything": 1}
	}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "test-key")

	var decoded *gen.ReportStatePayload
	handler := agentserver.NewReportStateHandler(
		func(_ context.Context, payload any) (any, error) {
			var ok bool
			decoded, ok = payload.(*gen.ReportStatePayload)
			require.True(t, ok)
			return &gen.ReportStateResult{Stored: true, AppliedEtag: nil, ReceivedAt: "2026-10-26T12:00:01Z"}, nil
		},
		nil,
		goahttp.RequestDecoder,
		goahttp.ResponseEncoder,
		nil,
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotNil(t, decoded)
	require.Equal(t, "kernel", decoded.Tools[0].Layer)
}

func TestReportState_StoresSnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	report := stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5")
	report.EnforcementHalted = true
	report.DriftCountingSince = new("2026-10-26T08:00:00Z")
	report.Policy.WithheldPlugins = []*gen.AgentStateWithheldPlugin{
		{Marketplace: new("acme"), Slug: "linter", Reason: "user_disabled"},
	}
	report.Tools = []*gen.AgentStateTool{
		{Tool: "claude_code", Layer: "managed", Outcome: "armed", PendingReason: nil, ErrorKind: nil, RestartRequired: nil, ActionRequired: nil, DriftCorrections: new(3)},
		{Tool: "codex", Layer: "managed", Outcome: "pending", PendingReason: new("privileged_writer_not_ready"), ErrorKind: nil, RestartRequired: nil, ActionRequired: nil, DriftCorrections: nil},
		{Tool: "cursor", Layer: "user", Outcome: "error", PendingReason: nil, ErrorKind: new("permission_denied"), RestartRequired: nil, ActionRequired: new(true), DriftCorrections: nil},
		{Tool: "pi", Layer: "user", Outcome: "skipped", PendingReason: nil, ErrorKind: nil, RestartRequired: nil, ActionRequired: nil, DriftCorrections: nil},
	}
	report.Update = &gen.AgentStateUpdate{
		AutoUpdate:     new("automatic"),
		Channel:        new("stable"),
		PinnedTarget:   nil,
		CurrentBlocked: new(true),
		LastRollback:   &gen.AgentStateRollback{At: "2026-10-20T10:00:00Z", From: new("0.42.0"), To: new("0.41.3")},
	}

	res, err := ti.service.ReportState(ctx, report)
	require.NoError(t, err)
	require.True(t, res.Stored)
	require.Equal(t, "etag-1", conv.PtrValOr(res.AppliedEtag, ""))

	rows := stateReports(t, ti)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, conv.NormalizeEmail(mockidp.MockUserEmail), row.Email)
	require.Equal(t, "c02xl0gzjgh5", row.SerialNumber.String, "stored normalized, as the device heartbeat stores it")
	require.Equal(t, "etag-1", row.AppliedEtag.String)
	require.True(t, row.EnforcementHalted)
	require.True(t, row.CurrentBlocked)
	require.Equal(t, "automatic", row.AutoUpdate.String)
	require.Equal(t, "0.41.3", row.LastRollbackTo.String)
	require.Equal(t, "0.42.0", row.SpeakeasydVersion.String)
	// Pending and error count as degraded; skipped means not installed.
	require.Equal(t, int32(2), row.DegradedToolCount)

	var tools []map[string]any
	require.NoError(t, json.Unmarshal(row.Tools, &tools))
	require.Len(t, tools, 4)
	require.InDelta(t, 3, tools[0]["drift_corrections"], 0)
	require.NotContains(t, tools[1], "drift_corrections", "a tool that does not measure drift must stay distinguishable from zero drift")
	require.Equal(t, "permission_denied", tools[2]["error_kind"])

	var withheld []map[string]any
	require.NoError(t, json.Unmarshal(row.WithheldPlugins, &withheld))
	require.Equal(t, []map[string]any{{"marketplace": "acme", "slug": "linter", "reason": "user_disabled"}}, withheld)
}

func TestReportState_UnknownValuesStoredAsUnknown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	report := stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5")
	report.Tools = []*gen.AgentStateTool{
		{Tool: "claude_code", Layer: "kernel", Outcome: "quarantined", PendingReason: new("future_reason"), ErrorKind: new("future_kind"), RestartRequired: nil, ActionRequired: nil, DriftCorrections: nil},
	}
	_, err := ti.service.ReportState(ctx, report)
	require.NoError(t, err)

	var tools []map[string]any
	require.NoError(t, json.Unmarshal(stateReports(t, ti)[0].Tools, &tools))
	require.Equal(t, "unknown", tools[0]["layer"])
	require.Equal(t, "unknown", tools[0]["outcome"])
	require.Equal(t, "unknown", tools[0]["pending_reason"])
	require.Equal(t, "unknown", tools[0]["error_kind"])
}

func TestReportState_LatestReplacesPrevious(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.ReportState(ctx, stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5"))
	require.NoError(t, err)

	// Same machine, serial in a different case, no hostname: one row, the new
	// etag, and the known hostname kept.
	next := stateReport(mockidp.MockUserEmail, "c02xl0gzjgh5")
	next.Policy.AppliedEtag = new("etag-2")
	next.Hostname = nil
	_, err = ti.service.ReportState(ctx, next)
	require.NoError(t, err)

	rows := stateReports(t, ti)
	require.Len(t, rows, 1)
	require.Equal(t, "etag-2", rows[0].AppliedEtag.String)
	require.Equal(t, "devbox", rows[0].Hostname.String)
}

// The agent runs per OS user, so two people on one machine report different
// policy from one serial. Keying by serial alone would let them overwrite
// each other every tick.
func TestReportState_SharedMachineKeepsOneRowPerUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.ReportState(ctx, stateReport("alice@example.com", "C02XL0GZJGH5"))
	require.NoError(t, err)
	_, err = ti.service.ReportState(ctx, stateReport("bob@example.com", "C02XL0GZJGH5"))
	require.NoError(t, err)

	require.Len(t, stateReports(t, ti), 2)
}

func TestReportState_SerialLessMachineKeyedByEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	for _, etag := range []string{"etag-1", "etag-2"} {
		report := stateReport(mockidp.MockUserEmail, "")
		report.SerialNumber = nil
		report.Policy.AppliedEtag = new(etag)
		_, err := ti.service.ReportState(ctx, report)
		require.NoError(t, err)
	}

	rows := stateReports(t, ti)
	require.Len(t, rows, 1, "NULL serials must dedup, not pile up one row per report")
	require.False(t, rows[0].SerialNumber.Valid)
	require.Equal(t, "etag-2", rows[0].AppliedEtag.String)
}

func TestReportState_NonEndpointIsAcknowledgedNotStored(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"ephemeral", "server"} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAgentService(t)

			report := stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5")
			report.Environment = new(environment)
			res, err := ti.service.ReportState(ctx, report)
			require.NoError(t, err)
			require.False(t, res.Stored)
			require.Empty(t, stateReports(t, ti),
				"a cloud box must not attest the laptop of the person whose address it enrolled under")
		})
	}
}

func TestReportState_InstallKeyRequiresVouchedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	report := stateReport("", "C02XL0GZJGH5")
	report.Email = nil
	_, err := ti.service.ReportState(ctx, report)
	requireCode(t, err, oops.CodeBadRequest)
}

func TestReportState_PerUserKeyReportsAsKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, "owner@example.com")

	_, err := ti.service.ReportState(ctx, stateReport("someone-else@example.com", "C02XL0GZJGH5"))
	require.NoError(t, err)

	rows := stateReports(t, ti)
	require.Len(t, rows, 1)
	require.Equal(t, "owner@example.com", rows[0].Email)
}

func TestReportState_RateLimitedPerDevice(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ti.service.WithStateReportLimiter(ratelimit.New(ratelimit.NewRedisStore(ti.redis), "test-report-state-"+uuid.NewString(), ratelimit.PerMinute(1)))

	_, err := ti.service.ReportState(ctx, stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5"))
	require.NoError(t, err)
	_, err = ti.service.ReportState(ctx, stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5"))
	requireCode(t, err, oops.CodeRateLimitExceeded)
	_, err = ti.service.ReportState(ctx, stateReport(mockidp.MockUserEmail, "OTHERSERIAL1"))
	require.NoError(t, err, "another machine has its own budget")
	_, err = ti.service.ReportState(ctx, stateReport("someone-else@example.com", "C02XL0GZJGH5"))
	// A new vouched email must not reset the machine's budget.
	requireCode(t, err, oops.CodeRateLimitExceeded)
}

// A report that is acknowledged but not stored still spends the budget.
func TestReportState_NonEndpointReportsAreRateLimited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ti.service.WithStateReportLimiter(ratelimit.New(ratelimit.NewRedisStore(ti.redis), "test-report-state-"+uuid.NewString(), ratelimit.PerMinute(1)))

	report := stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5")
	report.Environment = new("ephemeral")
	_, err := ti.service.ReportState(ctx, report)
	require.NoError(t, err)
	_, err = ti.service.ReportState(ctx, report)
	requireCode(t, err, oops.CodeRateLimitExceeded)
}

func TestReportState_RefusesAgentKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, _ = withAgentKeyAuth(t, ctx, ti, "Report bot")

	_, err := ti.service.ReportState(ctx, stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5"))
	requireCode(t, err, oops.CodeForbidden)
	require.Empty(t, stateReports(t, ti))
}

func TestReportState_RefusesDeviceAgentToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	device := newWorkloadDevice(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})
	admitted, err := ti.service.JWTAuth(getPluginsContext(t), device.token, deviceAgentTokenScheme)
	require.NoError(t, err)

	_, err = ti.service.ReportState(admitted, stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5"))
	requireCode(t, err, oops.CodeForbidden)
	require.Empty(t, stateReports(t, ti))
}

func TestReportState_RejectsBlankWithheldSlug(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	report := stateReport(mockidp.MockUserEmail, "C02XL0GZJGH5")
	report.Policy.WithheldPlugins = []*gen.AgentStateWithheldPlugin{{Marketplace: nil, Slug: "   ", Reason: "user_disabled"}}
	_, err := ti.service.ReportState(ctx, report)
	requireCode(t, err, oops.CodeBadRequest)
}

func TestGetPlugins_RecordsServedEtag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	email := conv.NormalizeEmail(mockidp.MockUserEmail)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	served, err := testrepo.New(ti.conn).GetDeviceAgentServedEtagFixture(ctx, testrepo.GetDeviceAgentServedEtagFixtureParams{OrganizationID: ti.orgID, Email: email})
	require.NoError(t, err)
	require.Equal(t, res.Etag, served.ServedEtag.String)
	require.True(t, served.ServedEtagChangedAt.Valid)

	// An unchanged etag costs no write, so its change time stays put.
	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	again, err := testrepo.New(ti.conn).GetDeviceAgentServedEtagFixture(ctx, testrepo.GetDeviceAgentServedEtagFixtureParams{OrganizationID: ti.orgID, Email: email})
	require.NoError(t, err)
	require.Equal(t, served.ServedEtagChangedAt.Time, again.ServedEtagChangedAt.Time)

	// A new plugin changes the etag inside the heartbeat throttle window, and
	// the write must land anyway, or the device would read outdated.
	extra := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "extra-tool")
	assignPlugin(t, ctx, ti.conn, extra, ti.orgID, "user:"+mockidp.MockUserID)
	changed, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.NotEqual(t, res.Etag, changed.Etag)
	updated, err := testrepo.New(ti.conn).GetDeviceAgentServedEtagFixture(ctx, testrepo.GetDeviceAgentServedEtagFixtureParams{OrganizationID: ti.orgID, Email: email})
	require.NoError(t, err)
	require.Equal(t, changed.Etag, updated.ServedEtag.String)
	require.True(t, updated.ServedEtagChangedAt.Time.After(served.ServedEtagChangedAt.Time))
}
