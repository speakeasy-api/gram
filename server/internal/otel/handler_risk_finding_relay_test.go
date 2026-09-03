package otel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

var riskFindingRelayObservedAt = time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)

func TestRiskFindingRelayExportsSafeOTLPLog(t *testing.T) {
	t.Parallel()

	requests := make(chan *collectorlogsv1.ExportLogsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" {
			t.Errorf("request path = %q, want /v1/logs", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request := new(collectorlogsv1.ExportLogsServiceRequest)
		if err := proto.Unmarshal(body, request); err != nil {
			t.Errorf("unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- request
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	handler, projectID := newRiskFindingRelayTestHandler(t, server.URL, "include")
	finding := riskFindingRelayTestFinding(projectID)
	before := proto.Clone(finding)

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*riskv1.Finding]{{Message: finding}}))

	request := requireRiskFindingRelayRequest(t, requests)
	require.True(t, proto.Equal(before, finding), "relay must not mutate the shared source message")
	resourceLogs := requireOne(t, request.GetResourceLogs())
	require.Equal(t, "gram", otlpStringAttribute(resourceLogs.GetResource().GetAttributes(), string(attr.ServiceNameKey)))
	scopeLogs := requireOne(t, resourceLogs.GetScopeLogs())
	require.Equal(t, riskFindingScopeName, scopeLogs.GetScope().GetName())
	record := requireOne(t, scopeLogs.GetLogRecords())
	require.Equal(t, riskFindingEventName, record.GetEventName())
	require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_WARN, record.GetSeverityNumber())
	require.Equal(t, "WARN", record.GetSeverityText())
	require.Equal(t, riskFindingBody, record.GetBody().GetStringValue())
	createdAt, err := time.Parse(time.RFC3339, finding.GetCreatedAt())
	require.NoError(t, err)
	require.Equal(t, uint64(createdAt.UnixNano()), record.GetTimeUnixNano())
	require.Equal(t, uint64(riskFindingRelayObservedAt.UnixNano()), record.GetObservedTimeUnixNano())
	require.Equal(t, finding.GetId(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskFindingIDKey)))
	require.Equal(t, finding.GetOrganizationId(), otlpStringAttribute(record.GetAttributes(), string(attr.OrganizationIDKey)))
	require.Equal(t, projectID.String(), otlpStringAttribute(record.GetAttributes(), string(attr.ProjectIDKey)))
	require.Equal(t, finding.GetRiskPolicyId(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskPolicyIDKey)))
	require.Equal(t, finding.GetRiskPolicyVersion(), otlpIntAttribute(record.GetAttributes(), string(attr.RiskPolicyVersionKey)))
	require.Equal(t, finding.GetRuleId(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskRuleIDKey)))
	require.Equal(t, finding.GetSource(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskSourceKey)))
	require.InDelta(t, finding.GetConfidence(), otlpDoubleAttribute(record.GetAttributes(), string(attr.RiskConfidenceKey)), 0)
	require.Equal(t, int64(finding.GetStartPos()), otlpIntAttribute(record.GetAttributes(), string(attr.RiskStartPosKey)))
	require.Equal(t, int64(finding.GetEndPos()), otlpIntAttribute(record.GetAttributes(), string(attr.RiskEndPosKey)))
	require.Equal(t, finding.GetRequestId(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskScanRequestIDKey)))
	require.Equal(t, finding.GetChatMessageId(), otlpStringAttribute(record.GetAttributes(), string(attr.MessageIDKey)))
	require.Equal(t, finding.GetContentPartId(), otlpStringAttribute(record.GetAttributes(), string(attr.ChatContentPartIDKey)))
	require.Equal(t, finding.GetSurface(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskSurfaceKey)))
	require.Equal(t, finding.GetField(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskFieldKey)))
	require.Equal(t, finding.GetPath(), otlpStringAttribute(record.GetAttributes(), string(attr.RiskPathKey)))
	require.Equal(t, finding.GetToolCallId(), otlpStringAttribute(record.GetAttributes(), string(attr.GenAIToolCallIDKey)))
	require.Equal(t, []string{"credential", "high-confidence"}, otlpStringSliceAttribute(record.GetAttributes(), string(attr.RiskTagsKey)))

	wire, err := proto.Marshal(request)
	require.NoError(t, err)
	require.NotContains(t, string(wire), finding.GetMatch(), "raw matches must never enter the customer payload")
}

func TestRiskFindingRelayEligibility(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("0198cb4f-4840-70e6-9e1d-5558dc2d7ce1")
	tests := []struct {
		name   string
		mutate func(*riskv1.Finding)
		reason relayReason
		ok     bool
	}{

		{name: "new finding", mutate: func(f *riskv1.Finding) { f.SetEventKind("finding") }, ok: true},
		{name: "legacy new finding", mutate: func(f *riskv1.Finding) { f.SetEventKind("") }, ok: true},
		{name: "dead letter", mutate: func(f *riskv1.Finding) { f.SetDeadLetterReason("scanner failed") }, reason: relayReasonDeadLetter},
		{name: "suppression", mutate: func(f *riskv1.Finding) { f.SetEventKind("suppression") }, reason: relayReasonStateChange},
		{name: "unsuppression", mutate: func(f *riskv1.Finding) { f.SetEventKind("unsuppression") }, reason: relayReasonStateChange},
		{name: "legacy exclusion timestamp", mutate: func(f *riskv1.Finding) {
			f.SetEventKind("")
			f.SetExcludedAt("2026-08-20T12:00:00Z")
		}, reason: relayReasonStateChange},
		{name: "legacy false positive timestamp", mutate: func(f *riskv1.Finding) {
			f.SetEventKind("")
			f.SetFalsePositiveAt("2026-08-20T12:00:00Z")
		}, reason: relayReasonStateChange},
		{name: "unknown event", mutate: func(f *riskv1.Finding) { f.SetEventKind("changed") }, reason: relayReasonInvalid},
		{name: "invalid finding id", mutate: func(f *riskv1.Finding) { f.SetId("bad") }, reason: relayReasonInvalid},
		{name: "invalid timestamp", mutate: func(f *riskv1.Finding) { f.SetCreatedAt("bad") }, reason: relayReasonInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			finding := riskFindingRelayTestFinding(projectID)
			tt.mutate(finding)
			_, reason, ok := newRiskFindingRelayMessage(finding, nil)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.reason, reason)
		})
	}
}
func TestRiskFindingRelayNeverExportsMatchWithExcludePolicy(t *testing.T) {
	t.Parallel()

	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	handler, projectID := newRiskFindingRelayTestHandler(t, server.URL, "exclude")
	finding := riskFindingRelayTestFinding(projectID)
	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*riskv1.Finding]{{Message: finding}}))

	select {
	case body := <-bodies:
		require.NotContains(t, string(body), finding.GetMatch())
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for risk finding relay request")
	}
}

func TestRiskFindingRelayFailsClosedOnExclusionLookupError(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	handler, projectID := newRiskFindingRelayTestHandler(t, server.URL, "exclude")
	destination, err := handler.relay.destinationForRoute(t.Context(), relayRouteKey{
		organizationID: "org-test",
		projectID:      projectID,
	})
	require.NoError(t, err)
	require.NotNil(t, destination)
	handler.relay.db.Close()
	finding := riskFindingRelayTestFinding(projectID)
	message, _, ok := newRiskFindingRelayMessage(finding, nil)
	require.True(t, ok)
	var failed error
	message.fail = func(err error) { failed = err }

	require.NoError(t, handler.handleBatch(t.Context(), []riskFindingRelayMessage{message}))

	require.ErrorContains(t, failed, "evaluate risk finding exclusions")
	require.Zero(t, requests.Load())
}

func TestRiskFindingRelayAppliesExclusionsAddedBetweenBatches(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	handler, projectID := newRiskFindingRelayTestHandler(t, server.URL, "exclude")
	finding := riskFindingRelayTestFinding(projectID)
	require.NoError(t, testrepo.New(handler.relay.db).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 finding.GetOrganizationId(),
		Name:               "Risk Finding Relay Test",
		Slug:               "risk-finding-relay-test",
		GramAccountType:    "enterprise",
		WorkosID:           pgtype.Text{},
		Whitelisted:        false,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: riskFindingRelayObservedAt, Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: riskFindingRelayObservedAt.Add(14 * 24 * time.Hour), Valid: true},
		DisabledAt:         pgtype.Timestamptz{},
		CreatedAt:          pgtype.Timestamptz{},
	}))
	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*riskv1.Finding]{{Message: finding}}))
	require.Equal(t, int64(1), requests.Load())

	_, err := riskrepo.New(handler.relay.db).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      projectID,
		OrganizationID: finding.GetOrganizationId(),
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "exact",
		MatchValue:     finding.GetMatch(),
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*riskv1.Finding]{{Message: finding}}))

	require.Equal(t, int64(1), requests.Load())
}

func TestRiskFindingRelayIsolatesCollectorFailuresByProject(t *testing.T) {
	t.Parallel()

	var failingRequests atomic.Int64
	failingHeaders := make(chan string, 2)
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		failingRequests.Add(1)
		failingHeaders <- request.Header.Get("X-Project")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failingServer.Close)

	successRequests := make(chan *collectorlogsv1.ExportLogsServiceRequest, 1)
	successHeaders := make(chan string, 1)
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		successHeaders <- request.Header.Get("X-Project")
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		export := new(collectorlogsv1.ExportLogsServiceRequest)
		if err := proto.Unmarshal(body, export); err != nil {
			t.Errorf("unmarshal request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		successRequests <- export
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(successServer.Close)

	db, enc, productRelay := newRelayRouteTest(t, "/v1/logs")
	failingProjectID := createRelayTestProject(t, db, "org-test")
	successProjectID := createRelayTestProject(t, db, "org-test")
	failingDestination := createRelayTestDestination(
		t,
		db,
		"org-test",
		failingProjectID,
		failingServer.URL,
		encryptRelayTestHeaders(t, enc, map[string]string{"X-Project": "failing"}),
		"exclude",
	)
	successDestination := createRelayTestDestination(
		t,
		db,
		"org-test",
		successProjectID,
		successServer.URL,
		encryptRelayTestHeaders(t, enc, map[string]string{"X-Project": "success"}),
		"exclude",
	)
	createRelayTestRoute(t, db, "org-test", failingProjectID, dataexports.DataSourceRiskFindings, true, uuid.NullUUID{UUID: failingDestination.ID, Valid: true})
	createRelayTestRoute(t, db, "org-test", successProjectID, dataexports.DataSourceRiskFindings, true, uuid.NullUUID{UUID: successDestination.ID, Valid: true})
	handler := &RiskFindingRelayHandler{
		logger: testenv.NewLogger(t),
		relay:  newSignalRelay(db, enc, productRelay.policy, dataexports.DataSourceRiskFindings, "/v1/logs", "risk finding"),
		now:    func() time.Time { return riskFindingRelayObservedAt },
	}

	failing, _, ok := newRiskFindingRelayMessage(riskFindingRelayTestFinding(failingProjectID), nil)
	require.True(t, ok)
	successFinding := riskFindingRelayTestFinding(successProjectID)
	successFinding.SetId("0198cb4f-4840-70e6-9e1d-5558dc2d7cef")
	success, _, ok := newRiskFindingRelayMessage(successFinding, nil)
	require.True(t, ok)
	failingErrors := make(chan error, 1)
	successErrors := make(chan error, 1)
	failing.fail = func(err error) { failingErrors <- err }
	success.fail = func(err error) { successErrors <- err }

	require.NoError(t, handler.handleBatch(t.Context(), []riskFindingRelayMessage{failing, success}))

	require.Error(t, <-failingErrors)
	select {
	case err := <-successErrors:
		require.NoError(t, err)
	default:
	}
	require.Equal(t, int64(2), failingRequests.Load(), "Guardian retries the affected 5xx route once")
	require.Equal(t, "failing", <-failingHeaders)
	require.Equal(t, "failing", <-failingHeaders)
	require.Equal(t, "success", <-successHeaders)
	successExport := requireRiskFindingRelayRequest(t, successRequests)
	successRecord := requireOne(t, requireOne(t, successExport.GetResourceLogs()).GetScopeLogs()).GetLogRecords()[0]
	require.Equal(t, successProjectID.String(), otlpStringAttribute(successRecord.GetAttributes(), string(attr.ProjectIDKey)))
	require.Equal(t, successFinding.GetId(), otlpStringAttribute(successRecord.GetAttributes(), string(attr.RiskFindingIDKey)))
}

func TestRiskFindingRelayMapsMessageAndContentPartAnchors(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("0198cb4f-4840-70e6-9e1d-5558dc2d7ce1")
	tests := []struct {
		name                  string
		mutate                func(*riskv1.Finding)
		wantMessageAnchor     bool
		wantContentPartAnchor bool
	}{
		{
			name: "message anchored",
			mutate: func(finding *riskv1.Finding) {
				finding.SetContentPartId("")
			},
			wantMessageAnchor: true,
		},
		{
			name: "content part anchored",
			mutate: func(finding *riskv1.Finding) {
				finding.SetChatMessageId("")
			},
			wantContentPartAnchor: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			finding := riskFindingRelayTestFinding(projectID)
			tt.mutate(finding)
			message, _, ok := newRiskFindingRelayMessage(finding, nil)
			require.True(t, ok)
			record := requireOne(t, requireOne(t, buildRiskFindingRelayExport([]riskFindingRelayMessage{message}, riskFindingRelayObservedAt).GetResourceLogs()).GetScopeLogs()).GetLogRecords()[0]
			require.Equal(t, tt.wantMessageAnchor, hasOTLPAttribute(record.GetAttributes(), string(attr.MessageIDKey)))
			require.Equal(t, tt.wantContentPartAnchor, hasOTLPAttribute(record.GetAttributes(), string(attr.ChatContentPartIDKey)))
		})
	}
}

func TestRiskFindingRelayRightSizesExports(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("0198cb4f-4840-70e6-9e1d-5558dc2d7ce1")
	messages := make([]riskFindingRelayMessage, 3)
	for i := range messages {
		finding := riskFindingRelayTestFinding(projectID)
		finding.SetId(uuid.NewString())
		finding.SetTags([]string{strings.Repeat("x", maxLogRelayExportBytes/2)})
		message, _, ok := newRiskFindingRelayMessage(finding, nil)
		require.True(t, ok)
		messages[i] = message
	}

	batches, err := rightSizeProtoBatches(messages, maxLogRelayExportBytes, func(batch []riskFindingRelayMessage) (*collectorlogsv1.ExportLogsServiceRequest, error) {
		return buildRiskFindingRelayExport(batch, riskFindingRelayObservedAt), nil
	})
	require.NoError(t, err)
	require.Greater(t, len(batches), 1)
	for _, batch := range batches {
		require.LessOrEqual(t, proto.Size(batch.message), maxLogRelayExportBytes)
	}
}

func TestRiskFindingRelaySkipsExclusionLookupWithoutDestination(t *testing.T) {
	t.Parallel()

	db, enc, productRelay := newRelayRouteTest(t, "/v1/logs")
	projectID := createRelayTestProject(t, db, "org-test")
	handler := &RiskFindingRelayHandler{
		logger: testenv.NewLogger(t),
		relay:  newSignalRelay(db, enc, productRelay.policy, dataexports.DataSourceRiskFindings, "/v1/logs", "risk finding"),
		now:    func() time.Time { return riskFindingRelayObservedAt },
	}
	destination, err := handler.relay.destinationForRoute(t.Context(), relayRouteKey{
		organizationID: "org-test",
		projectID:      projectID,
	})
	require.NoError(t, err)
	require.Nil(t, destination)
	handler.relay.db.Close()

	require.NoError(t, handler.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*riskv1.Finding]{{Message: riskFindingRelayTestFinding(projectID)}}))
}

func TestRiskFindingRelayUsesGenericBody(t *testing.T) {
	t.Parallel()

	finding := riskFindingRelayTestFinding(uuid.MustParse("0198cb4f-4840-70e6-9e1d-5558dc2d7ce1"))
	finding.SetDescription("Detected personal account person@example.invalid")
	message, _, ok := newRiskFindingRelayMessage(finding, nil)
	require.True(t, ok)

	request := buildRiskFindingRelayExport([]riskFindingRelayMessage{message}, riskFindingRelayObservedAt)
	record := requireOne(t, requireOne(t, request.GetResourceLogs()).GetScopeLogs()).GetLogRecords()[0]
	require.Equal(t, riskFindingBody, record.GetBody().GetStringValue())
}

func newRiskFindingRelayTestHandler(t *testing.T, endpoint, sensitiveDataPolicy string) (*RiskFindingRelayHandler, uuid.UUID) {
	t.Helper()

	db, enc, productRelay := newRelayRouteTest(t, "/v1/logs")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(
		t,
		db,
		"org-test",
		projectID,
		endpoint,
		pgtype.Text{},
		sensitiveDataPolicy,
	)
	createRelayTestRoute(t, db, "org-test", projectID, dataexports.DataSourceRiskFindings, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	handler := &RiskFindingRelayHandler{
		logger: testenv.NewLogger(t),
		relay:  newSignalRelay(db, enc, productRelay.policy, dataexports.DataSourceRiskFindings, "/v1/logs", "risk finding"),
		now:    func() time.Time { return riskFindingRelayObservedAt },
	}
	return handler, projectID
}

func riskFindingRelayTestFinding(projectID uuid.UUID) *riskv1.Finding {
	return riskv1.Finding_builder{
		Id:                new("0198cb4f-4840-70e6-9e1d-5558dc2d7ce2"),
		RequestId:         new("0198cb4f-4840-70e6-9e1d-5558dc2d7ce3"),
		ChatMessageId:     new("0198cb4f-4840-70e6-9e1d-5558dc2d7ce4"),
		ProjectId:         new(projectID.String()),
		OrganizationId:    new("org-test"),
		RiskPolicyId:      new("0198cb4f-4840-70e6-9e1d-5558dc2d7ce5"),
		RiskPolicyVersion: new(int64(7)),
		CreatedAt:         new("2026-08-20T11:59:00Z"),
		RuleId:            new("secret.example"),
		Description:       new("Detected a credential"),
		Match:             new("raw-secret-must-not-leave-gram"),
		StartPos:          new(int32(4)),
		EndPos:            new(int32(35)),
		Tags:              []string{"credential", "high-confidence"},
		Source:            new("gitleaks"),
		Confidence:        new(0.99),
		ContentPartId:     new("0198cb4f-4840-70e6-9e1d-5558dc2d7ce6"),
		Surface:           new("tool_args"),
		Field:             new("arguments"),
		Path:              new("credentials.token"),
		ToolCallId:        new("call-123"),
		EventKind:         new("finding"),
	}.Build()
}

func requireOne[T any](t *testing.T, values []T) T {
	t.Helper()
	require.Len(t, values, 1)
	return values[0]
}

func requireRiskFindingRelayRequest(t *testing.T, requests <-chan *collectorlogsv1.ExportLogsServiceRequest) *collectorlogsv1.ExportLogsServiceRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for risk finding relay request")
		return nil
	}
}

func otlpStringAttribute(attributes []*commonv1.KeyValue, key string) string {
	for _, attribute := range attributes {
		if attribute.GetKey() == key {
			return attribute.GetValue().GetStringValue()
		}
	}
	return ""
}

func hasOTLPAttribute(attributes []*commonv1.KeyValue, key string) bool {
	for _, attribute := range attributes {
		if attribute.GetKey() == key {
			return true
		}
	}
	return false
}

func otlpIntAttribute(attributes []*commonv1.KeyValue, key string) int64 {
	for _, attribute := range attributes {
		if attribute.GetKey() == key {
			return attribute.GetValue().GetIntValue()
		}
	}
	return 0
}

func otlpDoubleAttribute(attributes []*commonv1.KeyValue, key string) float64 {
	for _, attribute := range attributes {
		if attribute.GetKey() == key {
			return attribute.GetValue().GetDoubleValue()
		}
	}
	return 0
}

func otlpStringSliceAttribute(attributes []*commonv1.KeyValue, key string) []string {
	for _, attribute := range attributes {
		if attribute.GetKey() != key {
			continue
		}
		values := attribute.GetValue().GetArrayValue().GetValues()
		result := make([]string, len(values))
		for i, value := range values {
			result[i] = value.GetStringValue()
		}
		return result
	}
	return nil
}

func TestRiskFindingRelayPayloadContainsNoMatchAttribute(t *testing.T) {
	t.Parallel()

	finding := riskFindingRelayTestFinding(uuid.MustParse("0198cb4f-4840-70e6-9e1d-5558dc2d7ce1"))
	message, _, ok := newRiskFindingRelayMessage(finding, nil)
	require.True(t, ok)
	request := buildRiskFindingRelayExport([]riskFindingRelayMessage{message}, riskFindingRelayObservedAt)
	record := requireOne(t, requireOne(t, request.GetResourceLogs()).GetScopeLogs()).GetLogRecords()[0]
	for _, attribute := range record.GetAttributes() {
		require.NotContains(t, attribute.GetKey(), "match")
	}
}
