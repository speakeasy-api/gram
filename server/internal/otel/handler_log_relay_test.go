package otel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedLogRelayRequest struct {
	customer    string
	path        string
	contentType string
	bodySize    int
	request     *collectorlogsv1.ExportLogsServiceRequest
	err         error
}

type logRelayRequestCapture struct {
	mu       sync.Mutex
	requests []capturedLogRelayRequest
}

type logRelayFeatureFlagCall struct {
	flag       feature.Flag
	distinctID string
	groups     map[string]string
}

type logRelayFeatureFlagResult struct {
	enabled bool
	err     error
}

type logRelayTestFeatureProvider struct {
	mu            sync.Mutex
	results       map[string]logRelayFeatureFlagResult
	defaultResult logRelayFeatureFlagResult
	calls         []logRelayFeatureFlagCall
}

func (p *logRelayTestFeatureProvider) IsFlagEnabled(_ context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, logRelayFeatureFlagCall{
		flag:       flag,
		distinctID: distinctID,
		groups:     groups,
	})
	result, ok := p.results[distinctID]
	if !ok {
		result = p.defaultResult
	}
	return result.enabled, result.err
}

func (p *logRelayTestFeatureProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (p *logRelayTestFeatureProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func (p *logRelayTestFeatureProvider) snapshotCalls() []logRelayFeatureFlagCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

func (p *logRelayTestFeatureProvider) setResult(distinctID string, result logRelayFeatureFlagResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.results == nil {
		p.results = make(map[string]logRelayFeatureFlagResult)
	}
	p.results[distinctID] = result
}

type logRelayOrganizationSlugResolverFunc func(context.Context, string) (string, error)

func (f logRelayOrganizationSlugResolverFunc) OrganizationSlug(ctx context.Context, organizationID string) (string, error) {
	return f(ctx, organizationID)
}

type logRelayOrganizationSlugResult struct {
	slug string
	err  error
}

type logRelayTestOrganizationSlugResolver struct {
	mu      sync.Mutex
	results map[string]logRelayOrganizationSlugResult
	calls   []string
	started chan<- struct{}
	release <-chan struct{}
}

func (r *logRelayTestOrganizationSlugResolver) OrganizationSlug(_ context.Context, organizationID string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, organizationID)
	result := r.results[organizationID]
	r.mu.Unlock()
	if r.started != nil {
		r.started <- struct{}{}
	}
	if r.release != nil {
		<-r.release
	}
	return result.slug, result.err
}

func (r *logRelayTestOrganizationSlugResolver) snapshotCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func (c *logRelayRequestCapture) handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	request := &collectorlogsv1.ExportLogsServiceRequest{}
	if err == nil {
		err = proto.Unmarshal(body, request)
	}

	c.mu.Lock()
	c.requests = append(c.requests, capturedLogRelayRequest{
		customer:    r.Header.Get("X-Customer"),
		path:        r.URL.Path,
		contentType: r.Header.Get("Content-Type"),
		bodySize:    len(body),
		request:     request,
		err:         err,
	})
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *logRelayRequestCapture) snapshot() []capturedLogRelayRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func TestLogRelayHandlerGatesMixedBatchByOrganization(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	flags := &logRelayTestFeatureProvider{
		mu: sync.Mutex{},
		results: map[string]logRelayFeatureFlagResult{
			testLogOrganizationID:      {enabled: true, err: nil},
			testLogOtherOrganizationID: {enabled: false, err: nil},
		},
		defaultResult: logRelayFeatureFlagResult{enabled: false, err: nil},
		calls:         nil,
	}
	organizations := &logRelayTestOrganizationSlugResolver{
		mu: sync.Mutex{},
		results: map[string]logRelayOrganizationSlugResult{
			testLogOrganizationID:      {slug: "enabled-org", err: nil},
			testLogOtherOrganizationID: {slug: "disabled-org", err: nil},
		},
		calls:   nil,
		started: nil,
		release: nil,
	}
	handler := newLogRelayTestHandlerWithFeatures(t, testenv.NewMeterProvider(t), flags, organizations)
	cacheLogRelayTestDestination(t, handler, testLogOrganizationID, server.URL, map[string]string{"X-Customer": "enabled"})
	cacheLogRelayTestDestination(t, handler, testLogOtherOrganizationID, server.URL, map[string]string{"X-Customer": "disabled"})

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("enabled-1", testLogOrganizationID, testLogProjectID, 0),
		relayTestLogRecord("disabled", testLogOtherOrganizationID, testLogOtherProjectID, 0),
		relayTestLogRecord("enabled-2", testLogOrganizationID, testLogOtherProjectID, 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	messages, failures = logRelayTestMessages(
		relayTestLogRecord("enabled-next-batch", testLogOrganizationID, testLogProjectID, 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 3)
	for _, request := range requests {
		require.Equal(t, "enabled", request.customer)
	}
	require.Equal(t, []logRelayFeatureFlagCall{
		{
			flag:       feature.FlagOTELLogCustomerRelay,
			distinctID: testLogOrganizationID,
			groups:     feature.OrgProjectGroups("enabled-org", ""),
		},
		{
			flag:       feature.FlagOTELLogCustomerRelay,
			distinctID: testLogOtherOrganizationID,
			groups:     feature.OrgProjectGroups("disabled-org", ""),
		},
	}, flags.snapshotCalls())
	require.Equal(t, []string{testLogOrganizationID, testLogOtherOrganizationID}, organizations.snapshotCalls())
}

func TestLogRelayHandlerIsolatesFlagEvaluationFailureWithinBatch(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	flags := &logRelayTestFeatureProvider{
		mu: sync.Mutex{},
		results: map[string]logRelayFeatureFlagResult{
			testLogOrganizationID:      {enabled: true, err: nil},
			testLogOtherOrganizationID: {enabled: false, err: errors.New("evaluate flag")},
		},
		defaultResult: logRelayFeatureFlagResult{enabled: false, err: nil},
		calls:         nil,
	}
	organizations := &logRelayTestOrganizationSlugResolver{
		mu: sync.Mutex{},
		results: map[string]logRelayOrganizationSlugResult{
			testLogOrganizationID:      {slug: "enabled-org", err: nil},
			testLogOtherOrganizationID: {slug: "error-org", err: nil},
			testLogThirdOrganizationID: {slug: "", err: errors.New("resolve organization")},
		},
		calls:   nil,
		started: nil,
		release: nil,
	}
	handler := newLogRelayTestHandlerWithFeatures(t, testenv.NewMeterProvider(t), flags, organizations)
	cacheLogRelayTestDestination(t, handler, testLogOrganizationID, server.URL, map[string]string{"X-Customer": "enabled"})
	cacheLogRelayTestDestination(t, handler, testLogOtherOrganizationID, server.URL, map[string]string{"X-Customer": "error"})
	cacheLogRelayTestDestination(t, handler, testLogThirdOrganizationID, server.URL, map[string]string{"X-Customer": "missing"})

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("enabled", testLogOrganizationID, testLogProjectID, 0),
		relayTestLogRecord("evaluation-error", testLogOtherOrganizationID, testLogOtherProjectID, 0),
		relayTestLogRecord("missing-flag", testLogThirdOrganizationID, testLogThirdProjectID, 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	messages, failures = logRelayTestMessages(
		relayTestLogRecord("evaluation-error-next-batch", testLogOtherOrganizationID, testLogOtherProjectID, 0),
		relayTestLogRecord("organization-error-next-batch", testLogThirdOrganizationID, testLogThirdProjectID, 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "enabled", requests[0].customer)
	require.Equal(t, []logRelayFeatureFlagCall{
		{
			flag:       feature.FlagOTELLogCustomerRelay,
			distinctID: testLogOrganizationID,
			groups:     feature.OrgProjectGroups("enabled-org", ""),
		},
		{
			flag:       feature.FlagOTELLogCustomerRelay,
			distinctID: testLogOtherOrganizationID,
			groups:     feature.OrgProjectGroups("error-org", ""),
		},
		{
			flag:       feature.FlagOTELLogCustomerRelay,
			distinctID: testLogOtherOrganizationID,
			groups:     feature.OrgProjectGroups("error-org", ""),
		},
	}, flags.snapshotCalls())
	require.Equal(
		t,
		[]string{
			testLogOrganizationID,
			testLogOtherOrganizationID,
			testLogThirdOrganizationID,
			testLogOtherOrganizationID,
			testLogThirdOrganizationID,
		},
		organizations.snapshotCalls(),
	)
}

func TestLogRelayOrganizationGateCoalescesConcurrentLookups(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	flags := &logRelayTestFeatureProvider{
		mu: sync.Mutex{},
		results: map[string]logRelayFeatureFlagResult{
			testLogOrganizationID: {enabled: true, err: nil},
		},
		defaultResult: logRelayFeatureFlagResult{enabled: false, err: nil},
		calls:         nil,
	}
	organizations := &logRelayTestOrganizationSlugResolver{
		mu: sync.Mutex{},
		results: map[string]logRelayOrganizationSlugResult{
			testLogOrganizationID: {slug: "enabled-org", err: nil},
		},
		calls:   nil,
		started: started,
		release: release,
	}
	gate := newCachedLogRelayOrganizationGate(
		testenv.NewLogger(t),
		flags,
		organizations,
		logRelayOrganizationGateMaxSize,
		logRelayOrganizationGateCacheTTL,
		logRelayOrganizationGateLookupTimeout,
	)

	const callers = 32
	begin := make(chan struct{})
	results := make(chan bool, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-begin
			results <- gate.Enabled(t.Context(), testLogOrganizationID)
		}()
	}
	ready.Wait()
	close(begin)
	<-started
	close(release)

	for range callers {
		require.True(t, <-results)
	}
	require.Equal(t, []string{testLogOrganizationID}, organizations.snapshotCalls())
	require.Len(t, flags.snapshotCalls(), 1)
}

func TestLogRelayOrganizationGateCallerCancellationDoesNotCancelSharedLookup(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var organizationCalls atomic.Int64
	organizations := logRelayOrganizationSlugResolverFunc(func(ctx context.Context, _ string) (string, error) {
		if organizationCalls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return "enabled-org", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	flags := &logRelayTestFeatureProvider{
		mu: sync.Mutex{},
		results: map[string]logRelayFeatureFlagResult{
			testLogOrganizationID: {enabled: true, err: nil},
		},
		defaultResult: logRelayFeatureFlagResult{enabled: false, err: nil},
		calls:         nil,
	}
	gate := newCachedLogRelayOrganizationGate(
		testenv.NewLogger(t),
		flags,
		organizations,
		logRelayOrganizationGateMaxSize,
		logRelayOrganizationGateCacheTTL,
		time.Second,
	)

	callerCtx, cancelCaller := context.WithCancel(t.Context())
	firstResult := make(chan bool, 1)
	go func() {
		firstResult <- gate.Enabled(callerCtx, testLogOrganizationID)
	}()
	<-started
	cancelCaller()
	require.False(t, <-firstResult)

	secondResult := make(chan bool, 1)
	go func() {
		secondResult <- gate.Enabled(t.Context(), testLogOrganizationID)
	}()
	close(release)
	require.True(t, <-secondResult)
	require.True(t, gate.Enabled(t.Context(), testLogOrganizationID))
	require.Equal(t, int64(1), organizationCalls.Load())
	require.Len(t, flags.snapshotCalls(), 1)
}

func TestLogRelayOrganizationGateBoundsLookupAndDoesNotCacheTimeout(t *testing.T) {
	t.Parallel()

	finished := make(chan struct{})
	var organizationCalls atomic.Int64
	organizations := logRelayOrganizationSlugResolverFunc(func(ctx context.Context, _ string) (string, error) {
		if organizationCalls.Add(1) == 1 {
			<-ctx.Done()
			close(finished)
			return "", ctx.Err()
		}
		return "enabled-org", nil
	})
	flags := &logRelayTestFeatureProvider{
		mu: sync.Mutex{},
		results: map[string]logRelayFeatureFlagResult{
			testLogOrganizationID: {enabled: true, err: nil},
		},
		defaultResult: logRelayFeatureFlagResult{enabled: false, err: nil},
		calls:         nil,
	}
	gate := newCachedLogRelayOrganizationGate(
		testenv.NewLogger(t),
		flags,
		organizations,
		logRelayOrganizationGateMaxSize,
		logRelayOrganizationGateCacheTTL,
		50*time.Millisecond,
	)

	require.False(t, gate.Enabled(t.Context(), testLogOrganizationID))
	<-finished
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		assert.True(collect, gate.Enabled(t.Context(), testLogOrganizationID))
	}, time.Second, 5*time.Millisecond)
	require.True(t, gate.Enabled(t.Context(), testLogOrganizationID))
	require.Equal(t, int64(2), organizationCalls.Load())
	require.Len(t, flags.snapshotCalls(), 1)
}

func TestLogRelayOrganizationGateDoesNotCacheFeatureProviderError(t *testing.T) {
	t.Parallel()

	var organizationCalls atomic.Int64
	organizations := logRelayOrganizationSlugResolverFunc(func(context.Context, string) (string, error) {
		organizationCalls.Add(1)
		return "enabled-org", nil
	})
	flags := &logRelayTestFeatureProvider{
		mu: sync.Mutex{},
		results: map[string]logRelayFeatureFlagResult{
			testLogOrganizationID: {enabled: false, err: errors.New("evaluate flag")},
		},
		defaultResult: logRelayFeatureFlagResult{enabled: false, err: nil},
		calls:         nil,
	}
	gate := newCachedLogRelayOrganizationGate(
		testenv.NewLogger(t),
		flags,
		organizations,
		logRelayOrganizationGateMaxSize,
		logRelayOrganizationGateCacheTTL,
		time.Second,
	)

	require.False(t, gate.Enabled(t.Context(), testLogOrganizationID))
	flags.setResult(testLogOrganizationID, logRelayFeatureFlagResult{enabled: true, err: nil})
	require.True(t, gate.Enabled(t.Context(), testLogOrganizationID))
	require.True(t, gate.Enabled(t.Context(), testLogOrganizationID))
	require.Equal(t, int64(2), organizationCalls.Load())
	require.Len(t, flags.snapshotCalls(), 2)
}

func TestLogRelayOrganizationGateBoundsCachedOrganizations(t *testing.T) {
	t.Parallel()

	flags := &logRelayTestFeatureProvider{
		mu:            sync.Mutex{},
		results:       nil,
		defaultResult: logRelayFeatureFlagResult{enabled: true, err: nil},
		calls:         nil,
	}
	organizations := &logRelayTestOrganizationSlugResolver{
		mu: sync.Mutex{},
		results: map[string]logRelayOrganizationSlugResult{
			"organization-1": {slug: "org-1", err: nil},
			"organization-2": {slug: "org-2", err: nil},
			"organization-3": {slug: "org-3", err: nil},
		},
		calls:   nil,
		started: nil,
		release: nil,
	}
	gate := newCachedLogRelayOrganizationGate(
		testenv.NewLogger(t),
		flags,
		organizations,
		2,
		time.Hour,
		logRelayOrganizationGateLookupTimeout,
	)

	require.True(t, gate.Enabled(t.Context(), "organization-1"))
	require.True(t, gate.Enabled(t.Context(), "organization-2"))
	require.True(t, gate.Enabled(t.Context(), "organization-3"))
	require.True(t, gate.Enabled(t.Context(), "organization-1"))

	require.Equal(
		t,
		[]string{"organization-1", "organization-2", "organization-3", "organization-1"},
		organizations.snapshotCalls(),
	)
	require.Len(t, flags.snapshotCalls(), 4)
	require.Equal(t, 2, gate.enabled.Len())
}

func TestLogRelayHandlerGroupsByProvenanceAndCachesDestinations(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	destinationA := cacheLogRelayTestDestination(t, handler, testLogOrganizationID, server.URL, map[string]string{"X-Customer": "a"})
	cacheLogRelayTestDestination(t, handler, testLogOtherOrganizationID, server.URL, map[string]string{"X-Customer": "b"})
	require.Equal(t, 10*time.Second, destinationA.httpClient.Timeout)

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("a-1", testLogOrganizationID, testLogProjectID, 0),
		relayTestLogRecord("a-2", testLogOrganizationID, testLogProjectID, 0),
		relayTestLogRecord("a-3", testLogOrganizationID, testLogOtherProjectID, 0),
		relayTestLogRecord("b-1", testLogOtherOrganizationID, testLogProjectID, 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	messages, failures = logRelayTestMessages(relayTestLogRecord("a-4", testLogOrganizationID, testLogProjectID, 0))
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 4)
	groupedBodies := make(map[string][]string)
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.Equal(t, "/v1/logs", captured.path)
		require.Equal(t, "application/x-protobuf", captured.contentType)

		bodies := relayRequestLogBodies(captured.request)
		slices.Sort(bodies)
		groupedBodies[captured.customer] = append(groupedBodies[captured.customer], bodies...)
		if slices.Equal(bodies, []string{"a-1", "a-2"}) {
			require.Len(t, captured.request.GetResourceLogs(), 1)
			require.Len(t, captured.request.GetResourceLogs()[0].GetScopeLogs(), 1)
			require.Len(t, captured.request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords(), 2)
		}
	}
	slices.Sort(groupedBodies["a"])
	slices.Sort(groupedBodies["b"])
	require.Equal(t, []string{"a-1", "a-2", "a-3", "a-4"}, groupedBodies["a"])
	require.Equal(t, []string{"b-1"}, groupedBodies["b"])
}

func TestLogRelayHandlerRightSizesLargeBatchWithoutMixingOrganizations(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))

	type organizationSpec struct {
		customer       string
		organizationID string
		projectID      string
		recordCount    int
	}
	specs := []organizationSpec{
		{customer: "a", organizationID: testLogOrganizationID, projectID: testLogProjectID, recordCount: 5},
		{customer: "b", organizationID: testLogOtherOrganizationID, projectID: testLogOtherProjectID, recordCount: 4},
		{customer: "c", organizationID: testLogThirdOrganizationID, projectID: testLogThirdProjectID, recordCount: 7},
	}

	recordBodyBytes := maxOTLPLogRecordBytes / 3
	expectedNames := make(map[string][]string, len(specs))
	records := make([]*otelv1.LogRecord, 0, 16)
	for _, spec := range specs {
		cacheLogRelayTestDestination(t, handler, spec.organizationID, server.URL, map[string]string{"X-Customer": spec.customer})
		for index := range spec.recordCount {
			name := fmt.Sprintf("%s-%d", spec.customer, index)
			record := relayTestLogRecord(name, spec.organizationID, spec.projectID, recordBodyBytes)
			require.LessOrEqual(t, proto.Size(record), maxOTLPLogRecordBytes)
			records = append(records, record)
			expectedNames[spec.customer] = append(expectedNames[spec.customer], name)
		}
	}

	messages, failures := logRelayTestMessages(records...)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	deliveredNames := make(map[string][]string, len(specs))
	batchSizes := make(map[string][]int, len(specs))
	requests := capture.snapshot()
	require.Len(t, requests, 7)
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.LessOrEqual(t, captured.bodySize, maxLogRelayExportBytes)
		names := relayRequestLogEventNames(captured.request)
		require.NotEmpty(t, names)
		for _, name := range names {
			require.Contains(t, expectedNames[captured.customer], name)
		}
		deliveredNames[captured.customer] = append(deliveredNames[captured.customer], names...)
		batchSizes[captured.customer] = append(batchSizes[captured.customer], len(names))
	}

	for customer, expected := range expectedNames {
		slices.Sort(expected)
		slices.Sort(deliveredNames[customer])
		slices.Sort(batchSizes[customer])
		require.Equal(t, expected, deliveredNames[customer])
	}
	require.Equal(t, []int{2, 3}, batchSizes["a"])
	require.Equal(t, []int{1, 3}, batchSizes["b"])
	require.Equal(t, []int{1, 3, 3}, batchSizes["c"])
}

func TestLogRelayHandlerLimitsDestinationRequestsToFourMiB(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheLogRelayTestDestination(t, handler, testLogOrganizationID, server.URL, nil)

	recordBodyBytes := maxOTLPLogRecordBytes / 2
	records := []*otelv1.LogRecord{
		relayTestLogRecord("large-1", testLogOrganizationID, testLogProjectID, recordBodyBytes),
		relayTestLogRecord("large-2", testLogOrganizationID, testLogProjectID, recordBodyBytes),
		relayTestLogRecord("large-3", testLogOrganizationID, testLogProjectID, recordBodyBytes),
		relayTestLogRecord("large-4", testLogOrganizationID, testLogProjectID, recordBodyBytes),
		relayTestLogRecord("large-5", testLogOrganizationID, testLogProjectID, recordBodyBytes),
	}
	for _, record := range records {
		require.LessOrEqual(t, proto.Size(record), maxOTLPLogRecordBytes)
	}
	messages, failures := logRelayTestMessages(records...)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 3)
	deliveredRecords := 0
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.LessOrEqual(t, captured.bodySize, maxLogRelayExportBytes)
		for _, resourceLogs := range captured.request.GetResourceLogs() {
			for _, scopeLogs := range resourceLogs.GetScopeLogs() {
				deliveredRecords += len(scopeLogs.GetLogRecords())
			}
		}
	}
	require.Equal(t, len(records), deliveredRecords)
}

func TestLogRelayDestinationRejectsRequestOverFourMiB(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	destination := cacheLogRelayTestDestination(t, handler, testLogOrganizationID, server.URL, nil)
	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{
		relayTestLogRecord("oversized", testLogOrganizationID, testLogProjectID, maxLogRelayExportBytes),
	})
	require.NoError(t, err)

	err = destination.exportWithLimit(t.Context(), request, maxLogRelayExportBytes)

	require.ErrorContains(t, err, "limit is 4194304")
	require.Zero(t, requests.Load())
}

func TestLogRelayHandlerFailsMessagesForRetryableDestination(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	handler := newLogRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheLogRelayTestDestination(t, handler, testLogOrganizationID, server.URL, nil)

	messages, failures := logRelayTestMessages(
		relayTestLogRecord("failed", testLogOrganizationID, testLogProjectID, 0),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.ErrorContains(t, failures[0], "503 Service Unavailable")
	require.Equal(t, int64(2), requests.Load(), "Guardian should make one retry before Pub/Sub stages redelivery")
}

func TestNewLogRelayExportRequestDiscardsGramOnlyFields(t *testing.T) {
	t.Parallel()

	record := relayTestLogRecord("visible", testLogOrganizationID, testLogProjectID, 0)
	futureOTLPField := protowire.AppendTag(nil, 13, protowire.BytesType)
	futureOTLPField = protowire.AppendString(futureOTLPField, "future-otlp-value")
	futureGramField := protowire.AppendTag(nil, 1006, protowire.BytesType)
	futureGramField = protowire.AppendString(futureGramField, "internal-future-value")
	record.ProtoReflect().SetUnknown(append(futureOTLPField, futureGramField...))

	request, err := newLogRelayExportRequest([]*otelv1.LogRecord{record})
	require.NoError(t, err)
	require.Len(t, request.GetResourceLogs(), 1)
	require.Len(t, request.GetResourceLogs()[0].GetScopeLogs(), 1)
	require.Len(t, request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords(), 1)

	converted := request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0]
	require.Equal(t, futureOTLPField, []byte(converted.ProtoReflect().GetUnknown()))

	encoded, err := proto.Marshal(request)
	require.NoError(t, err)
	for _, internalValue := range []string{
		"record-visible",
		testLogOrganizationID,
		testLogProjectID,
		"internal-future-value",
	} {
		require.NotContains(t, string(encoded), internalValue)
	}
}

func logRelayTestMessages(records ...*otelv1.LogRecord) ([]logRelayMessage, []error) {
	failures := make([]error, len(records))
	messages := make([]logRelayMessage, len(records))
	for i, record := range records {
		index := i
		messages[i] = logRelayMessage{
			record: record,
			fail: func(err error) {
				failures[index] = err
			},
		}
	}
	return messages, failures
}

func newLogRelayTestHandler(t *testing.T, meterProvider metric.MeterProvider) *LogRelayHandler {
	t.Helper()
	features := &logRelayTestFeatureProvider{
		mu:            sync.Mutex{},
		results:       nil,
		defaultResult: logRelayFeatureFlagResult{enabled: true, err: nil},
		calls:         nil,
	}
	organizations := &logRelayTestOrganizationSlugResolver{
		mu: sync.Mutex{},
		results: map[string]logRelayOrganizationSlugResult{
			testLogOrganizationID:      {slug: "org-a", err: nil},
			testLogOtherOrganizationID: {slug: "org-b", err: nil},
			testLogThirdOrganizationID: {slug: "org-c", err: nil},
		},
		calls:   nil,
		started: nil,
		release: nil,
	}
	return newLogRelayTestHandlerWithFeatures(t, meterProvider, features, organizations)
}

func newLogRelayTestHandlerWithFeatures(
	t *testing.T,
	meterProvider metric.MeterProvider,
	features feature.Provider,
	organizations logRelayOrganizationSlugResolver,
) *LogRelayHandler {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	handler := NewLogRelayHandler(
		testenv.NewLogger(t),
		meterProvider,
		nil,
		testenv.NewEncryptionClient(t),
		policy,
		features,
	)
	handler.organizationGate = newCachedLogRelayOrganizationGate(
		testenv.NewLogger(t),
		features,
		organizations,
		logRelayOrganizationGateMaxSize,
		logRelayOrganizationGateCacheTTL,
		logRelayOrganizationGateLookupTimeout,
	)
	return handler
}

func cacheLogRelayTestDestination(
	t *testing.T,
	handler *LogRelayHandler,
	organizationID string,
	baseURL string,
	headers map[string]string,
) *relayDestination {
	t.Helper()
	destination, err := handler.relay.newDestination(organizationID, baseURL, headers)
	require.NoError(t, err)
	handler.relay.destinationCache[organizationID] = cachedRelayDestination{
		destination: destination,
		expiresAt:   time.Now().Add(time.Hour),
	}
	return destination
}

func relayTestLogRecord(body, organizationID, projectID string, bodyBytes int) *otelv1.LogRecord {
	resourceSchemaURL := "https://opentelemetry.io/schemas/1.27.0"
	scopeSchemaURL := "https://opentelemetry.io/schemas/1.28.0"
	var recordBody *otelv1.LogRecord_AnyValue
	if bodyBytes > 0 {
		recordBody = (&otelv1.LogRecord_AnyValue_builder{BytesValue: make([]byte, bodyBytes)}).Build()
	} else {
		recordBody = (&otelv1.LogRecord_AnyValue_builder{StringValue: &body}).Build()
	}
	recordID := "record-" + body
	return (&otelv1.LogRecord_builder{
		RecordId:          &recordID,
		Body:              recordBody,
		EventName:         &body,
		Resource:          (&otelv1.LogRecord_Resource_builder{Attributes: nil}).Build(),
		ResourceSchemaUrl: &resourceSchemaURL,
		Scope: (&otelv1.LogRecord_InstrumentationScope_builder{
			Name: new("com.speakeasy.ai.logging"),
		}).Build(),
		ScopeSchemaUrl: &scopeSchemaURL,
		Provenance: (&otelv1.LogRecord_Provenance_builder{
			Source:         new("test"),
			OrganizationId: &organizationID,
			ProjectId:      &projectID,
		}).Build(),
	}).Build()
}

func relayRequestLogBodies(request *collectorlogsv1.ExportLogsServiceRequest) []string {
	var bodies []string
	for _, resourceLogs := range request.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				bodies = append(bodies, record.GetBody().GetStringValue())
			}
		}
	}
	return bodies
}

func relayRequestLogEventNames(request *collectorlogsv1.ExportLogsServiceRequest) []string {
	var names []string
	for _, resourceLogs := range request.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				names = append(names, record.GetEventName())
			}
		}
	}
	return names
}
