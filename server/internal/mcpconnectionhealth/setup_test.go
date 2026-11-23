package mcpconnectionhealth_test

import (
	"context"
	"log"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service *mcpconnectionhealth.Service
	conn    *pgxpool.Pool
	ch      driver.Conn
	flags   *targetedFlag
	logsOn  *atomic.Bool
	org     string
	project uuid.UUID
	userID  string
	issuer  uuid.UUID
	server  uuid.UUID
	now     time.Time
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "mcp_connection_health")
	require.NoError(t, err)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 12)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	flags := &targetedFlag{}
	flags.targetOrganizations(authCtx.OrganizationSlug)
	logsOn := &atomic.Bool{}
	logsOn.Store(true)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := mcpconnectionhealth.NewService(logger, tracerProvider, conn, ch, sessionManager, authzEngine, flags,
		func(context.Context, string) (bool, error) { return logsOn.Load(), nil })

	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		OrganizationID:     pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:               "default-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: 0, Days: 14, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	server, err := metamcprepo.New(conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                "Linear",
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	return ctx, &testInstance{
		service: service,
		conn:    conn,
		ch:      ch,
		flags:   flags,
		logsOn:  logsOn,
		org:     authCtx.ActiveOrganizationID,
		project: *authCtx.ProjectID,
		userID:  authCtx.UserID,
		issuer:  issuer.ID,
		server:  server.ID,
		now:     time.Now().UTC(),
	}
}

// targetedFlag models a PostHog flag that is EITHER a group flag (organization
// slugs, person ignored) OR a person flag (emails, groups ignored). A person
// flag given no email falls back to looking the distinct ID up as a stored person.
type targetedFlag struct {
	mu               sync.Mutex
	orgSlugs         []string
	emails           []string
	err              error
	distinctID       string
	personProperties map[string]string
}

func (f *targetedFlag) targetOrganizations(slugs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orgSlugs, f.emails = slugs, nil
}

func (f *targetedFlag) targetPeople(emails ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orgSlugs, f.emails = nil, emails
}

func (f *targetedFlag) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// lastCall returns the distinct ID and person properties last evaluated.
func (f *targetedFlag) lastCall() (string, map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.distinctID, f.personProperties
}

func (f *targetedFlag) EvaluateFlag(_ context.Context, flag feature.Flag, distinctID string, groups, personProperties map[string]string) (feature.Evaluation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.distinctID, f.personProperties = distinctID, personProperties
	if flag != feature.FlagMCPConnectionHealth {
		return feature.EvaluationIndeterminate, nil
	}
	if f.err != nil {
		return feature.EvaluationIndeterminate, f.err
	}
	matched := false
	if f.emails != nil {
		email, supplied := personProperties["email"]
		if !supplied {
			email = distinctID
		}
		matched = slices.Contains(f.emails, email)
	} else {
		matched = slices.Contains(f.orgSlugs, groups["organization"])
	}
	if matched {
		return feature.EvaluationEnabled, nil
	}
	return feature.EvaluationDisabled, nil
}

func (*targetedFlag) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (*targetedFlag) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (*targetedFlag) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

// insert writes one finished attempt per step and result.
func (ti *testInstance) insert(t *testing.T, org string, project uuid.UUID, started time.Time, step mcpconnectionattempts.Step, what mcpconnectionattempts.Happened, opts ...mcpconnectionattempts.Option) uuid.UUID {
	t.Helper()
	a := mcpconnectionattempts.AttemptAt(uuid.New(), started, mcpconnectionattempts.GatewayScope(org, project, ti.server, mcpconnectionattempts.AppDCR))
	row, err := mcpconnectionattempts.RowAt(a, step, what, true, started.Add(time.Second), opts...)
	require.NoError(t, err)
	require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ti.ch, []mcpconnectionattempts.Row{row}))
	return a.ID()
}

func (ti *testInstance) rangeFrom() string { return ti.now.Add(-24 * time.Hour).Format(time.RFC3339) }
func (ti *testInstance) rangeTo() string   { return ti.now.Add(time.Minute).Format(time.RFC3339) }

func withProjectGrants(t *testing.T, ctx context.Context, scopes ...authz.Scope) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	grants := make([]authz.Grant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, authCtx.ProjectID.String()))
	}
	return authztest.WithExactGrants(t, ctx, grants...)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
