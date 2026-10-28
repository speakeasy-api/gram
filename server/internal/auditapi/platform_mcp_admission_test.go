package auditapi_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auditapi"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type auditQueryRecorder struct {
	reads    atomic.Int64
	projects atomic.Int64
	failMask atomic.Bool
}

func (r *auditQueryRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: ListAuditLogs ") {
		r.reads.Add(1)
	}
	if strings.HasPrefix(data.SQL, "-- name: AuditProjectBelongsToOrganization ") {
		r.projects.Add(1)
	}
	if r.failMask.Load() && strings.HasPrefix(data.SQL, "-- name: FilterOrganizationMemberUserIDs ") {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		return canceled
	}
	return ctx
}

func (*auditQueryRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPlatformMCPAuditAdmissionPrecedesProtectedQueries(t *testing.T) { //nolint:tparallel // Denial subtests must finish before the shared recorder's positive control and masking failure.
	t.Parallel()
	ctx, ti := newTestAuditService(t)
	auth := *testAuthContext(t, ctx)
	auth.SessionID = nil
	base := contextvalues.WithAuthenticatedActor(t.Context(), &auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
	base = contextvalues.SetActingSurface(base, contextvalues.ActingSurfacePlatformMCP)
	_, grantsLoaded := authz.GrantsFromContext(base)
	require.False(t, grantsLoaded, "base must have no grant snapshot, unlike the loaded-but-empty required denial case")
	read := authztest.WithExactGrants(t, base, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	request := auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, Cursor: ""}
	recorder := &auditQueryRecorder{}
	config := ti.conn.Config()
	config.ConnConfig.Tracer = recorder
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, pool, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := auditapi.NewService(logger, testenv.NewTracerProvider(t), pool, ti.sessionManager, engine)
	otherActor := contextvalues.WithAuthenticatedActor(read, &auth, urn.NewPrincipal(urn.PrincipalTypeUser, "other-user"))
	for name, deniedContext := range map[string]context.Context{ //nolint:paralleltest // Sequential cases share the query counter with the positive control below.
		"missing grants":        base,
		"required denial":       authztest.WithExactGrants(t, base),
		"foreign read":          authztest.WithExactGrants(t, base, authz.NewGrant(authz.ScopeOrgRead, "other-org")),
		"identity mismatch":     otherActor,
		"enforcement bypass":    contextvalues.SetActingSurface(read, "dashboard"),
		"authorization failure": contextvalues.WithLegacyAPIKeyAuthorization(read, &auth),
	} {
		t.Run(name, func(t *testing.T) {
			projectRequest := request
			projectRequest.ProjectID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			page, err := service.ListPlatformMCPAuditLogs(deniedContext, projectRequest)
			require.Error(t, err)
			require.Nil(t, page)
			require.Zero(t, recorder.reads.Load())
			require.Zero(t, recorder.projects.Load())
		})
	}
	conflicting := request
	conflicting.ActorID = "other-user"
	conflicting.ProjectID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	page, err := service.ListPlatformMCPAuditLogs(read, conflicting)
	require.Error(t, err)
	require.Nil(t, page)
	require.Zero(t, recorder.projects.Load(), "another actor refuses before project validation")
	require.Zero(t, recorder.reads.Load())
	request.ActorID = auth.UserID
	page, err = service.ListPlatformMCPAuditLogs(read, request)
	require.NoError(t, err)
	require.NotNil(t, page)
	require.EqualValues(t, 1, recorder.reads.Load(), "the recorder observes a successful protected read")
	insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: auth.ActiveOrganizationID, actorID: auth.UserID, actorType: "user", action: "project:update", subjectID: "project", subjectType: "project",
	})
	recorder.failMask.Store(true)
	page, err = service.ListPlatformMCPAuditLogs(read, request)
	require.Error(t, err, "masking lookup failure refuses the whole page")
	require.Nil(t, page)
	require.EqualValues(t, 2, recorder.reads.Load())
}
