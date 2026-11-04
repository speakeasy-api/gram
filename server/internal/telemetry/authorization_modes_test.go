package telemetry_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// TestAuthorizationModesAgree runs tool-log, project and organization reads
// through the engine, a shadow comparison and the registry evaluator: every
// mode must allow and deny the same callers.
func TestAuthorizationModesAgree(t *testing.T) {
	t.Parallel()

	for _, mode := range []authz.MigrationMode{authz.MigrationModeOld, authz.MigrationModeShadow, authz.MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestLogsService(t)
			engine := authz.NewEngine(ti.logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
			enabled := func(context.Context, string) (bool, error) { return true, nil }
			// Shadow mode uses the parity client, so a disagreement fails the
			// test instead of only being logged.
			authorization := authztest.NewParityClient(t, engine)
			if mode != authz.MigrationModeShadow {
				authorization = authztest.NewClientInMode(t, engine, mode)
			}
			svc := telemetry.NewService(ti.logger, testenv.NewTracerProvider(t), ti.conn, ti.chConn, nil, nil, enabled, enabled, nil,
				engine, authorization, ti.featureFlags)

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := authCtx.ProjectID.String()
			organizationID := authCtx.ActiveOrganizationID
			now := time.Now().UTC().Truncate(time.Second)

			searchLogs := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := svc.SearchLogs(authztest.WithExactGrants(t, ctx, grants...), &telem_gen.SearchLogsPayload{Limit: 10, Sort: "desc"}); err != nil {
					return fmt.Errorf("search logs: %w", err)
				}
				return nil
			}
			overview := func(t *testing.T, grants ...authz.Grant) error {
				t.Helper()
				if _, err := svc.GetProjectOverview(authztest.WithExactGrants(t, ctx, grants...), &telem_gen.GetProjectOverviewPayload{
					From: now.Add(-24 * time.Hour).Format(time.RFC3339),
					To:   now.Format(time.RFC3339),
				}); err != nil {
					return fmt.Errorf("project overview: %w", err)
				}
				return nil
			}

			require.NoError(t, searchLogs(t, authz.NewGrant(authz.ScopeLogRead, projectID)))
			requireForbidden(t, searchLogs(t, authz.NewGrant(authz.ScopeProjectWrite, projectID)))
			requireForbidden(t, searchLogs(t, authz.NewGrant(authz.ScopeOrgAdmin, organizationID)))
			requireForbidden(t, searchLogs(t, authz.NewGrant(authz.ScopeLogRead, "other-project")))

			require.NoError(t, overview(t, authz.NewGrant(authz.ScopeProjectRead, projectID)))
			requireForbidden(t, overview(t, authz.NewGrant(authz.ScopeLogRead, projectID)))
			requireForbidden(t, overview(t,
				authz.NewGrant(authz.ScopeProjectRead, projectID),
				authz.NewGrant(authz.ScopeProjectBlockedRead, projectID),
			))
		})
	}
}
