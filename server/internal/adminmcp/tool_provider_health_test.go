package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

type recordingProviderHealthReader struct {
	*recordingOrganizationReader
	query   *mcpconnectionattempts.ProviderHealthQuery
	reports []mcpconnectionattempts.ProviderHealthReport
	err     error
}

func (r *recordingProviderHealthReader) ReadProviderHealth(_ context.Context, query mcpconnectionattempts.ProviderHealthQuery) ([]mcpconnectionattempts.ProviderHealthReport, error) {
	r.query = &query
	return r.reports, r.err
}

func callProviderHealthTool(t *testing.T, reads OrganizationReader, verified bool, args string) (string, json.RawMessage, bool) {
	t.Helper()
	principal := staffPrincipal()
	if verified {
		principal.staff = &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
	}
	handler := NewRuntime(&testAuthenticator{principal: principal}, "", reads).Handler()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"describe_provider_health","arguments":`+args+`}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
			IsError           bool            `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Body.String(), message.Result.StructuredContent, message.Result.IsError
}

func testProviderReports() []mcpconnectionattempts.ProviderHealthReport {
	return []mcpconnectionattempts.ProviderHealthReport{
		{
			Provider: "accounts.google.com",
			Status:   mcpconnectionattempts.ProviderOutage,
			Counts:   mcpconnectionattempts.ProviderCounts{Attempts: 10, Failures: 6, Organizations: 3, BaselineAttempts: 100, BaselineFailures: 1},
			Failures: []mcpconnectionattempts.ProviderFailure{
				{Step: mcpconnectionattempts.StepProviderRefresh.String(), Reason: mcpconnectionattempts.CodeProviderUnavailable.String(), Attempts: 6},
			},
			ReconnectLoops: 2,
			Abandoned:      []mcpconnectionattempts.ProviderStepCount{{Step: mcpconnectionattempts.StepProviderSignIn.String(), Attempts: 1}},
			Daily: []mcpconnectionattempts.ProviderDay{
				{Day: time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC), Provider: "accounts.google.com", Attempts: 20, Failed: 3, Abandoned: 1, Fixed: 2},
			},
		},
		{Provider: mcpconnectionattempts.LabelOther, Status: mcpconnectionattempts.ProviderNotChecked},
	}
}

func TestDescribeProviderHealth_ReturnsPlainCounts(t *testing.T) {
	t.Parallel()

	reads := &recordingProviderHealthReader{recordingOrganizationReader: &recordingOrganizationReader{}, reports: testProviderReports()}
	_, data, isError := callProviderHealthTool(t, reads, true, `{}`)
	require.False(t, isError)
	require.Equal(t, mcpconnectionattempts.ProviderHealthQuery{Provider: "", Window: 24 * time.Hour, TrendDays: 14}, *reads.query)

	var out ProviderHealthOutput
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, 24, out.WindowHours)
	require.Equal(t, ProviderHealthThresholds{StatusWindowMinutes: 60, BaselineDays: 7, MinProviderFailures: 5, MinOrganizations: 3, RateMultiple: 5, RateIncrease: 0.2}, out.Thresholds)
	require.Len(t, out.Providers, 2)
	require.Equal(t, ProviderHealthEntry{
		Provider: "accounts.google.com",
		Status:   "outage",
		Summary:  "accounts.google.com looks down: 6 of 10 attempts in the last hour failed on its side, in 3 organizations. Usually 1% fail.",
		LastHour: ProviderHealthLastHour{Attempts: 10, ProviderFailures: 6, OrganizationsAffected: 3, FailureRate: 0.6, UsualFailureRate: new(0.01)},
		Failures: []ProviderHealthFailure{{
			Step: "provider_refresh", Reason: "provider_unavailable", WhatHappened: "The service is temporarily unavailable.", WhoFixes: "provider", Attempts: 6,
		}},
		ReconnectLoops: 2,
		Abandoned:      []ProviderHealthStep{{Step: "provider_sign_in", Attempts: 1}},
		Daily:          []ProviderHealthDay{{Day: "2026-11-04", Attempts: 20, Failed: 3, Abandoned: 1, Fixed: 2}},
	}, out.Providers[0])
	require.Equal(t, "not_checked", out.Providers[1].Status)
	require.Equal(t, "Providers not on the known list are counted together as other and not checked for outages.", out.Providers[1].Summary)
}

func TestDescribeProviderHealth_PassesItsBounds(t *testing.T) {
	t.Parallel()

	reads := &recordingProviderHealthReader{recordingOrganizationReader: &recordingOrganizationReader{}, reports: nil}
	_, _, isError := callProviderHealthTool(t, reads, true, `{"provider":"other","window_hours":168,"trend_days":0}`)
	require.False(t, isError)
	require.Equal(t, mcpconnectionattempts.ProviderHealthQuery{Provider: "other", Window: 168 * time.Hour, TrendDays: 0}, *reads.query)
}

func TestDescribeProviderHealth_RefusesBadInput(t *testing.T) {
	t.Parallel()

	for name, args := range map[string]string{
		"tenant host":     `{"provider":"acme.okta.com"}`,
		"unlisted window": `{"window_hours":5}`,
		"trend too long":  `{"trend_days":396}`,
		"negative trend":  `{"trend_days":-1}`,
		"negative window": `{"window_hours":-1}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reads := &recordingProviderHealthReader{recordingOrganizationReader: &recordingOrganizationReader{}, reports: nil}
			_, _, isError := callProviderHealthTool(t, reads, true, args)
			require.True(t, isError)
			require.Nil(t, reads.query, "nothing is read")
		})
	}
}

func TestDescribeProviderHealth_ReaderErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	reads := &recordingProviderHealthReader{recordingOrganizationReader: &recordingOrganizationReader{}, err: errors.New("clickhouse said org-secret")}
	body, _, isError := callProviderHealthTool(t, reads, true, `{}`)
	require.True(t, isError)
	require.Contains(t, body, errProviderHealthUnavailable.Error())
	require.NotContains(t, body, "org-secret")
}

func TestDescribeProviderHealth_NeedsVerifiedStaff(t *testing.T) {
	t.Parallel()

	reads := &recordingProviderHealthReader{recordingOrganizationReader: &recordingOrganizationReader{}, reports: testProviderReports()}
	_, _, isError := callProviderHealthTool(t, reads, false, `{}`)
	require.True(t, isError)
	require.Nil(t, reads.query)
}

func TestDescribeProviderHealth_UnavailableWithoutAReader(t *testing.T) {
	t.Parallel()

	body, _, isError := callProviderHealthTool(t, &recordingOrganizationReader{}, true, `{}`)
	require.True(t, isError)
	require.Contains(t, body, errProviderHealthUnavailable.Error())
}

func TestPercent_KeepsASmallRateVisible(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0%", percent(0))
	require.Equal(t, "0.4%", percent(0.004))
	require.Equal(t, "25%", percent(0.25))
}

func TestDescribeProviderHealth_SaysWhenThereIsNoUsualRate(t *testing.T) {
	t.Parallel()

	reads := &recordingProviderHealthReader{recordingOrganizationReader: &recordingOrganizationReader{}, reports: []mcpconnectionattempts.ProviderHealthReport{{
		Provider: "mcp.linear.app",
		Status:   mcpconnectionattempts.ProviderElevated,
		Counts:   mcpconnectionattempts.ProviderCounts{Attempts: 1, Failures: 1, Organizations: 1, BaselineAttempts: 0, BaselineFailures: 0},
		Failures: []mcpconnectionattempts.ProviderFailure{{Step: "provider_token", Reason: mcpconnectionattempts.LabelOther, Attempts: 1}},
	}}}
	body, data, isError := callProviderHealthTool(t, reads, true, `{}`)
	require.False(t, isError)
	require.Contains(t, body, `"usual_failure_rate":null`)

	var out ProviderHealthOutput
	require.NoError(t, json.Unmarshal(data, &out))
	entry := out.Providers[0]
	require.Nil(t, entry.LastHour.UsualFailureRate)
	require.Equal(t, "mcp.linear.app is failing more than usual: 1 of 1 attempt in the last hour failed on its side, in 1 organization. There is no usual rate yet.", entry.Summary)
	require.Equal(t, []ProviderHealthFailure{{Step: "provider_token", Reason: "other", WhatHappened: "Unknown reason.", WhoFixes: "unknown", Attempts: 1}}, entry.Failures)
}
