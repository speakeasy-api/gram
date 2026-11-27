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
	// logsOffAfter, when set, turns logs off after that many checks.
	logsOffAfter *atomic.Int32
	org          string
	project      uuid.UUID
	userID       string
	issuer       uuid.UUID
	server       uuid.UUID
	now          time.Time
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
	logsOffAfter, logsChecks := &atomic.Int32{}, &atomic.Int32{}
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := mcpconnectionhealth.NewService(logger, tracerProvider, conn, ch, sessionManager, authzEngine, flags,
		func(context.Context, string) (bool, error) {
			if after := logsOffAfter.Load(); after > 0 && logsChecks.Add(1) > after {
				return false, nil
			}
			return logsOn.Load(), nil
		})

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
		service:      service,
		conn:         conn,
		ch:           ch,
		flags:        flags,
		logsOn:       logsOn,
		logsOffAfter: logsOffAfter,
		org:          authCtx.ActiveOrganizationID,
		project:      *authCtx.ProjectID,
		userID:       authCtx.UserID,
		issuer:       issuer.ID,
		server:       server.ID,
		now:          time.Now().UTC(),
	}
}

// flagCall is one evaluation the service asked for.
type flagCall struct {
	distinctID       string
	personProperties map[string]string
}

// targetedFlag models PostHog for FlagMCPConnectionHealth. The flag is EITHER a
// group flag (organization slugs, person ignored) OR a person flag (emails,
// groups ignored). The local check decides a person flag only from a supplied
// email; the organization check (EvaluateFlag) sends no email, so a person
// flag there looks the distinct ID up as a stored person. delegate, when set,
// replaces the model with a real provider.
type targetedFlag struct {
	mu        sync.Mutex
	orgSlugs  []string
	emails    []string
	noLocal   bool
	err       error
	delegate  feature.Provider
	local     []flagCall
	evaluated []flagCall
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

// withoutLocalEvaluation models a provider with no personal API key.
func (f *targetedFlag) withoutLocalEvaluation() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.noLocal = true
}

func (f *targetedFlag) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *targetedFlag) use(provider feature.Provider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delegate = provider
}

// calls returns and clears the local and organization evaluations so far.
func (f *targetedFlag) calls() (local, evaluated []flagCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	local, evaluated = f.local, f.evaluated
	f.local, f.evaluated = nil, nil
	return local, evaluated
}

func (f *targetedFlag) IsFlagEnabledLocal(ctx context.Context, flag feature.Flag, distinctID string, groups, personProperties map[string]string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.local = append(f.local, flagCall{distinctID: distinctID, personProperties: personProperties})
	if f.delegate != nil {
		return f.delegate.IsFlagEnabledLocal(ctx, flag, distinctID, groups, personProperties) //nolint:wrapcheck // Test double.
	}
	if flag != feature.FlagMCPConnectionHealth || f.noLocal {
		return false, nil
	}
	if f.err != nil {
		return false, f.err
	}
	if f.emails != nil {
		email, supplied := personProperties["email"]
		return supplied && slices.Contains(f.emails, email), nil
	}
	return slices.Contains(f.orgSlugs, groups["organization"]), nil
}

func (f *targetedFlag) EvaluateFlag(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evaluated = append(f.evaluated, flagCall{distinctID: distinctID, personProperties: nil})
	if f.delegate != nil {
		return feature.EvaluateFlag(ctx, f.delegate, flag, distinctID, groups) //nolint:wrapcheck // Test double.
	}
	switch {
	case flag != feature.FlagMCPConnectionHealth:
		return feature.EvaluationIndeterminate, nil
	case f.err != nil:
		return feature.EvaluationIndeterminate, f.err
	case f.emails != nil && slices.Contains(f.emails, distinctID), f.emails == nil && slices.Contains(f.orgSlugs, groups["organization"]):
		return feature.EvaluationEnabled, nil
	default:
		return feature.EvaluationDisabled, nil
	}
}

func (f *targetedFlag) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	if f.delegate != nil {
		return f.delegate.IsFlagEnabled(ctx, flag, distinctID, groups) //nolint:wrapcheck // Test double.
	}
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
