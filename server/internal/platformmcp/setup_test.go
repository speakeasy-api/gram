package platformmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type signalAuthoringFixture struct {
	service   *SignalAuthoringService
	principal Principal
	project   ResolvedProject
	features  *productfeatures.Client
}

func newSignalAuthoringFixture(t *testing.T) (context.Context, signalAuthoringFixture) {
	t.Helper()
	db, err := platformMCPInfra.CloneTestDatabase(t, "signal_authoring")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	logger, tracer := testenv.NewLogger(t), testenv.NewTracerProvider(t)
	redis, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(logger, tracer, db, redis)
	require.NoError(t, features.SetFeatureEnabled(t.Context(), principal.OrganizationID, productfeatures.FeatureSignalsIntelligence, true))
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := sigint.NewService(logger, tracer, db, &sessions.Manager{}, engine, audit.NewLogger(), features)
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &project.ID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextWithPrincipal(ctx, principal)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String()), authz.NewGrant(authz.ScopeProjectWrite, project.ID.String())})
	return ctx, signalAuthoringFixture{service: NewSignalAuthoringService(management, NewPostgresReader(logger, db), engine, "test-signal-authoring-key"), principal: principal, project: project, features: features}
}
