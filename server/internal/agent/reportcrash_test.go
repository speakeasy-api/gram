package agent_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

func crashReport(id string) *gen.AgentCrashReport {
	return &gen.AgentCrashReport{
		SchemaVersion:   1,
		ID:              id,
		Kind:            "fatal",
		Component:       "runtime",
		OccurredAt:      "2026-10-27T12:00:00Z",
		Count:           1,
		Build:           &gen.AgentCrashBuild{Binary: "speakeasyd", Version: "0.42.0", Channel: nil, Os: nil, Arch: nil},
		Message:         new("fatal error: concurrent map writes"),
		PanicType:       nil,
		Stack:           []*gen.AgentCrashFrame{{Function: "runtime.throw", File: nil, Line: nil}},
		OtherGoroutines: new(12),
		Rollback:        nil,
	}
}

func crashPayload(email *string, reports ...*gen.AgentCrashReport) *gen.ReportCrashPayload {
	return &gen.ReportCrashPayload{
		ApikeyToken:  nil,
		Reports:      reports,
		Email:        email,
		SerialNumber: new("C02ABC"),
		Environment:  nil,
	}
}

func TestReportCrash_AcceptsBatchFromPerUserKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	ctx = withPerUserKeyAuth(t, ctx, "dev@example.com")

	res, err := ti.service.ReportCrash(ctx, crashPayload(nil, crashReport("a"), crashReport("b")))
	require.NoError(t, err)
	require.Equal(t, 2, res.Accepted)
}

func TestReportCrash_OrgKeyNeedsVouchedEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	_, err := ti.service.ReportCrash(ctx, crashPayload(nil, crashReport("a")))
	requireCode(t, err, oops.CodeBadRequest)

	res, err := ti.service.ReportCrash(ctx, crashPayload(new("dev@example.com"), crashReport("a")))
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestReportCrash_RejectsNullReport(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	_, err := ti.service.ReportCrash(ctx, crashPayload(new("dev@example.com"), crashReport("a"), nil))
	requireCode(t, err, oops.CodeBadRequest)
}

func TestReportCrash_RateLimitedPerDevice(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	ti.service.WithCrashReportLimiter(ratelimit.New(ratelimit.NewRedisStore(ti.redis), "test-report-crash-"+uuid.NewString(), ratelimit.PerMinute(1)))
	payload := crashPayload(new("dev@example.com"), crashReport("a"))

	_, err := ti.service.ReportCrash(ctx, payload)
	require.NoError(t, err)
	// Another email on the same serial shares the device's budget.
	_, err = ti.service.ReportCrash(ctx, crashPayload(new("other@example.com"), crashReport("b")))
	requireCode(t, err, oops.CodeRateLimitExceeded)
}
