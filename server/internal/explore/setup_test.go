package explore

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var exploreTestInfra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch explore test infrastructure: %v", err)
	}
	exploreTestInfra = env

	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup explore test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newExploreTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := exploreTestInfra.CloneTestDatabase(t, "explore")
	require.NoError(t, err)
	return conn
}

func newExploreTestClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()

	conn, err := exploreTestInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	return conn
}

type exploreServiceTestInstance struct {
	service        *Service
	conn           *pgxpool.Pool
	organizationID string
}

func newExploreTestService(t *testing.T) (context.Context, *exploreServiceTestInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn := newExploreTestDB(t)
	chConn := newExploreTestClickhouse(t)
	redisClient, err := exploreTestInfra.NewRedisClient(t, 12)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-explore-test"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	service := NewService(logger, tracerProvider, conn, chConn, sessionManager, authzEngine, audit.NewLogger())
	return ctx, &exploreServiceTestInstance{
		service:        service,
		conn:           conn,
		organizationID: authCtx.ActiveOrganizationID,
	}
}

type exploreOTelLog struct {
	OrganizationID string
	ProjectID      uuid.UUID
	OccurredAt     time.Time
	ObservedAt     time.Time
	Source         string
	EventName      string
	Body           string
	Attributes     map[string]any
}

func insertExploreOTelLog(t *testing.T, conn clickhouse.Conn, log exploreOTelLog) {
	t.Helper()

	organizationID := log.OrganizationID
	if organizationID == "" {
		organizationID = "org-explore-promotion-test"
	}
	observedAt := log.ObservedAt
	if observedAt.IsZero() {
		observedAt = log.OccurredAt
	}
	body := log.Body
	if body == "" {
		body = "explore promotion test"
	}
	attributesJSON, err := json.Marshal(log.Attributes)
	require.NoError(t, err)

	err = conn.Exec(t.Context(), `
		INSERT INTO otel_logs (
			organization_id, project_id, time_unix_nano,
			observed_time_unix_nano, source, trace_id, span_id, event_name,
			severity_text, severity_number, body, log_attributes, flags,
			resource_attributes, resource_schema_url, scope_name, scope_version,
			scope_attributes
		) VALUES (?, ?, ?, ?, ?, '', '', ?, 'INFO', 9, ?, ?, 0, '{}', '', 'com.speakeasy.ai.logging', '', '{}')
	`,
		organizationID,
		log.ProjectID.String(),
		log.OccurredAt.UnixNano(),
		observedAt.UnixNano(),
		log.Source,
		log.EventName,
		body,
		string(attributesJSON),
	)
	require.NoError(t, err)
}

type exploreEventObservation struct {
	ProjectID     uuid.UUID
	NaturalID     string
	SourceEventID uuid.UUID
	OccurredAt    time.Time
	ObservedAt    time.Time
	SourceChannel string
	EventName     any
	Provider      any
	RequestModel  any
	ResponseModel any
	ToolName      any
}

func insertExploreEventObservation(t *testing.T, conn clickhouse.Conn, event exploreEventObservation) {
	t.Helper()

	err := conn.Exec(t.Context(), `
		INSERT INTO chat_events (
			project_id, natural_id, src_event_id, occurred_at, observed_at,
			source_channel, event_name, provider, surface,
			account_type, user_key, session_id, turn_id, query_source,
			request_model, response_model,
			tool_name, mcp_server, skill_name, status, terminal, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, NULL, ?, ?, ?, NULL, NULL, NULL, NULL, NULL)
	`, event.ProjectID, event.NaturalID, event.SourceEventID, event.OccurredAt,
		event.ObservedAt, event.SourceChannel, event.EventName, event.Provider,
		event.RequestModel, event.ResponseModel, event.ToolName)
	require.NoError(t, err)
}

type exploreMeasurementObservation struct {
	ProjectID        uuid.UUID
	MeasurementName  string
	NaturalID        string
	ComponentID      string
	SourceEventID    uuid.UUID
	OccurredAt       time.Time
	ObservedAt       time.Time
	SourceChannel    string
	ObservationKind  string
	Granularity      any
	Provider         any
	Surface          any
	AccountType      any
	UserKey          any
	SessionID        any
	TurnID           any
	QuerySource      any
	RequestModel     any
	ResponseModel    any
	CostUSD          any
	InputTokens      any
	OutputTokens     any
	CacheReadTokens  any
	CacheWriteTokens any
}

func insertExploreMeasurementObservation(t *testing.T, conn Conn, observation exploreMeasurementObservation) {
	t.Helper()

	err := conn.Exec(t.Context(), `
		INSERT INTO chat_measurements (
			project_id, measurement_name, natural_id, component_id, src_event_id,
			occurred_at, observed_at, source_channel, observation_kind, granularity,
			provider, surface, account_type, user_key, session_id, turn_id,
			query_source, request_model, response_model, cost_usd, input_tokens, output_tokens,
			cache_read_tokens, cache_write_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		observation.ProjectID,
		observation.MeasurementName,
		observation.NaturalID,
		observation.ComponentID,
		observation.SourceEventID,
		observation.OccurredAt,
		observation.ObservedAt,
		observation.SourceChannel,
		observation.ObservationKind,
		observation.Granularity,
		observation.Provider,
		observation.Surface,
		observation.AccountType,
		observation.UserKey,
		observation.SessionID,
		observation.TurnID,
		observation.QuerySource,
		observation.RequestModel,
		observation.ResponseModel,
		observation.CostUSD,
		observation.InputTokens,
		observation.OutputTokens,
		observation.CacheReadTokens,
		observation.CacheWriteTokens,
	)
	require.NoError(t, err)
}

func createOrganizationFixture(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	now := time.Now().UTC()
	err := testrepo.New(conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               "Explore Test Organization",
		Slug:               organizationID,
		GramAccountType:    "free",
		WorkosID:           conv.PtrToPGText(nil),
		Whitelisted:        false,
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(nil),
	})
	require.NoError(t, err)
}
