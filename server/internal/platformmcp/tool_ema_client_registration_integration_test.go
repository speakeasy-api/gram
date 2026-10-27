package platformmcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type emaRealFixture struct {
	conn     *pgxpool.Pool
	project  ResolvedProject
	issuerID uuid.UUID
	service  *EMAClientRegistrationService
}

// newEMARealFixture registers through the real management service, so the
// client, its audit row, and the receipt share the receipt transaction.
func newEMARealFixture(t *testing.T, name string) (context.Context, emaRealFixture) {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	ctx = emaPrincipalContext(t, ctx, principal)
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})

	issuer, err := usersessionsrepo.New(conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(principal.OrganizationID),
		Slug:                         "ema-" + name,
		AuthnChallengeMode:           "chain",
		SessionDuration:              pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{},
		TrustedRemoteSessionClientID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	management := usersessions.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, nil, engine, audit.NewLogger(), policy, nil,
		testenv.NewEncryptionClient(t), usersessions.NewSigner("test-jwt-secret"), "http://0.0.0.0", authserver.Hosts{ServerURL: "http://0.0.0.0"},
		ratelimit.NewRedisStore(redisClient), nil)

	return ctx, emaRealFixture{
		conn:     conn,
		project:  project,
		issuerID: issuer.ID,
		service: &EMAClientRegistrationService{
			management: management,
			trusted: func(ctx context.Context, organizationID string, issuerID uuid.UUID) (bool, error) {
				return usersessions.TrustedIdentityProviderConfigured(ctx, conn, organizationID, issuerID)
			},
			db:             conn,
			now:            time.Now,
			resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) { return project, nil },
		},
	}
}

func (f emaRealFixture) clientCount(t *testing.T, ctx context.Context) int {
	t.Helper()
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := usersessionsrepo.New(f.conn).ListUserSessionClientsByProjectID(ctx, usersessionsrepo.ListUserSessionClientsByProjectIDParams{
		ProjectID:           f.project.ID,
		OrganizationID:      ac.ActiveOrganizationID,
		UserSessionIssuerID: uuid.NullUUID{UUID: f.issuerID, Valid: true},
		Cursor:              uuid.NullUUID{},
		LimitValue:          100,
	})
	require.NoError(t, err)
	return len(rows)
}

func (f emaRealFixture) arguments(key string) map[string]any {
	return map[string]any{"issuer_id": f.issuerID.String(), "client_name": "Codex", "token_endpoint_auth_method": "client_secret_basic", "confirmed": true, "idempotency_key": key}
}

func TestEMAClientRegistrationReceiptFailureRollsBackClientAndAudit(t *testing.T) {
	t.Parallel()
	ctx, f := newEMARealFixture(t, "platform_mcp_ema_receipt_failure")
	auditBefore, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)

	// Completing the receipt fails after the client and audit rows were written.
	f.service.encodeReceipt = func(RegisterEMAClientOutput) ([]byte, error) { return nil, errors.New("receipt completion failed") }
	_, err = invokeEMAClientTool(t, ctx, f.service, f.arguments("codex-receipt"))
	requireEMAClientRefusal(t, err, unavailableCode)
	require.Equal(t, 0, f.clientCount(t, ctx))
	auditAfter, err := audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, auditBefore, auditAfter)

	f.service.encodeReceipt = nil
	first, err := invokeEMAClientTool(t, ctx, f.service, f.arguments("codex-receipt"))
	require.NoError(t, err)
	firstOut, ok := first.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.NotEmpty(t, firstOut.ClientSecret)
	require.False(t, firstOut.Replayed)

	replay, err := invokeEMAClientTool(t, ctx, f.service, f.arguments("codex-receipt"))
	require.NoError(t, err)
	replayOut, ok := replay.(RegisterEMAClientOutput)
	require.True(t, ok)
	require.True(t, replayOut.Replayed)
	require.Equal(t, firstOut.ClientID, replayOut.ClientID)
	require.Empty(t, replayOut.ClientSecret)

	require.Equal(t, 1, f.clientCount(t, ctx))
	auditAfter, err = audittest.AuditLogCountByAction(ctx, f.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter)

	different := f.arguments("codex-receipt")
	different["client_name"] = "Different"
	_, err = invokeEMAClientTool(t, ctx, f.service, different)
	requireEMAClientRefusal(t, err, "idempotency_key_reused")
	require.Equal(t, 1, f.clientCount(t, ctx))
}

func TestEMAClientRegistrationConcurrentSameKeyCreatesOneClient(t *testing.T) {
	t.Parallel()
	ctx, f := newEMARealFixture(t, "platform_mcp_ema_concurrent")

	const callers = 4
	var wg sync.WaitGroup
	var secrets atomic.Int32
	clientIDs := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() {
			result, err := invokeEMAClientTool(t, ctx, f.service, f.arguments("codex-concurrent"))
			errs[i] = err
			if out, ok := result.(RegisterEMAClientOutput); ok {
				clientIDs[i] = out.ClientID
				if out.ClientSecret != "" {
					secrets.Add(1)
				}
			}
		})
	}
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, clientIDs[0], clientIDs[i])
	}
	require.Equal(t, int32(1), secrets.Load(), "only the registering call returns the secret")
	require.Equal(t, 1, f.clientCount(t, ctx))
}
