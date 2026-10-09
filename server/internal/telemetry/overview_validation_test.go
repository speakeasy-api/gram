package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestGetObservabilityOverview_ValidatesUserFiltersBeforeCanonicalLookup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	// Keep authorization functional while making the telemetry service's
	// canonical lookup unavailable. Closing this separate pool leaves ti intact.
	unavailable, err := pgxpool.NewWithConfig(ctx, ti.conn.Config())
	require.NoError(t, err)
	unavailable.Close()
	engine := authz.NewEngine(ti.logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	logsEnabled := func(context.Context, string) (bool, error) { return true, nil }
	service := telemetry.NewService(ti.logger, testenv.NewTracerProvider(t), unavailable, ti.chConn,
		nil, nil, logsEnabled, logsEnabled, nil, engine, ti.featureFlags)
	now := time.Now().UTC()
	payload := &gen.GetObservabilityOverviewPayload{
		From: now.Add(-time.Hour).Format(time.RFC3339), To: now.Format(time.RFC3339),
		UserID: conv.PtrEmpty("test-user"), ExternalUserID: conv.PtrEmpty("external-user"),
		ToolsetSlug: conv.PtrEmpty("hosted-tools"),
	}
	_, err = service.GetObservabilityOverview(ctx, payload)
	var public *oops.ShareableError
	require.ErrorAs(t, err, &public)
	require.Equal(t, oops.CodeBadRequest, public.Code)
	require.Contains(t, err.Error(), "only one of user_id or external_user_id can be provided")

	// Prove that this fixture would reach the unavailable lookup for a valid
	// request, rather than accidentally bypassing it via an explicit server id.
	payload.ExternalUserID = nil
	_, err = service.GetObservabilityOverview(ctx, payload)
	require.ErrorAs(t, err, &public)
	require.Equal(t, oops.CodeUnexpected, public.Code)
	require.Contains(t, err.Error(), "error resolving hosted overview identity")
}
