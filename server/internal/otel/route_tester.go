package otel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const routeTestSignalTimeout = 5 * time.Second

const (
	// TestEventMessage is the body of every test log. Its prefix is how a
	// person reading a destination tells a test from real data.
	TestEventMessage = "Speakeasy test event: this is not real data."

	// TestEventName is the event name of every test log, and the span and metric name.
	TestEventName = "speakeasy.test_event"
)

// routeTestSignal is where a signal goes and its size limit, as its relay sends it.
type routeTestSignal struct {
	path     string
	maxBytes int
}

var routeTestSignals = map[dataexports.Signal]routeTestSignal{
	dataexports.SignalLogs:    {path: "/v1/logs", maxBytes: maxLogRelayExportBytes},
	dataexports.SignalTraces:  {path: "/v1/traces", maxBytes: 0},
	dataexports.SignalMetrics: {path: "/v1/metrics", maxBytes: maxMetricRelayExportBytes},
}

// RouteTestSource builds the test event for one data export source. It builds
// each record with the source's own resource, scope and request builder, so the
// test proves what real data would do. A request comes back already redacted
// for its destination and without the test marks, which the tester stamps.
type RouteTestSource interface {
	DataSource() string
	BuildTestRequest(ctx context.Context, signal dataexports.Signal, destination dataexports.TestDestination) (proto.Message, error)
}

// RouteTester sends a source's test events straight to a destination, one
// request per signal in parallel. It uses the relays' request path, outbound
// policy and client, with no retries and a short timeout, and publishes
// nothing, so no other sink sees a test.
type RouteTester struct {
	relays  map[dataexports.Signal]*signalRelay
	sources map[string]RouteTestSource
}

var _ dataexports.TestEventSender = (*RouteTester)(nil)

// NewRouteTester fails unless sources has exactly one entry for every
// registered data export source.
func NewRouteTester(policy *guardian.Policy, sources ...RouteTestSource) (*RouteTester, error) {
	if policy == nil {
		return nil, errors.New("route tester needs an outbound policy")
	}
	tester := &RouteTester{
		relays:  make(map[dataexports.Signal]*signalRelay, len(routeTestSignals)),
		sources: make(map[string]RouteTestSource, len(sources)),
	}
	for _, source := range sources {
		if source == nil {
			return nil, errors.New("route test source is nil")
		}
		name := source.DataSource()
		if _, ok := dataexports.LookupSource(name); !ok {
			return nil, fmt.Errorf("route test source %q is not a registered data export source", name)
		}
		if _, ok := tester.sources[name]; ok {
			return nil, fmt.Errorf("route test source %q is registered twice", name)
		}
		tester.sources[name] = source
	}
	for _, registered := range dataexports.Sources() {
		if _, ok := tester.sources[registered.Name]; !ok {
			return nil, fmt.Errorf("data export source %q has no route test source", registered.Name)
		}
		for _, signal := range registered.Signals {
			if _, ok := tester.relays[signal]; ok {
				continue
			}
			spec, ok := routeTestSignals[signal]
			if !ok {
				return nil, fmt.Errorf("data export signal %q has no OTLP path", signal)
			}
			relay := newSignalRelay(nil, nil, policy, "", spec.path, string(signal)+" test")
			relay.maxRetries = 0
			relay.timeout = routeTestSignalTimeout
			tester.relays[signal] = relay
		}
	}
	return tester, nil
}

// SendTest sends one test request per signal the registry lists for the source
// and returns a result for each, in registry order. Everything that can fail
// before sending is checked first, so once anything is sent every signal gets a
// status or is unreachable.
func (t *RouteTester) SendTest(ctx context.Context, sourceName string, destination dataexports.TestDestination, testEventID string) ([]dataexports.SignalTestResult, error) {
	registered, ok := dataexports.LookupSource(sourceName)
	source, built := t.sources[sourceName]
	if !ok || !built {
		return nil, fmt.Errorf("%w: %q", dataexports.ErrRouteTestUnsupported, sourceName)
	}
	if testEventID == "" {
		return nil, errors.New("a test needs a test event ID")
	}

	type preparedSend struct {
		target *relayDestination
		body   []byte
	}
	prepared := make([]preparedSend, len(registered.Signals))
	for i, signal := range registered.Signals {
		request, err := source.BuildTestRequest(ctx, signal, destination)
		if err != nil {
			return nil, fmt.Errorf("build %s %s test request: %w", sourceName, signal, err)
		}
		if err := stampTestRequest(request, testEventID); err != nil {
			return nil, fmt.Errorf("stamp %s %s test request: %w", sourceName, signal, err)
		}
		relay, ok := t.relays[signal]
		if !ok {
			return nil, fmt.Errorf("no relay for %s test", signal)
		}
		target, err := relay.newDestination(relayRouteKey{organizationID: destination.OrganizationID, projectID: destination.ProjectID}, destination.EndpointURL, destination.Headers, destination.IncludeSensitiveData)
		if err != nil {
			return nil, fmt.Errorf("build %s test destination: %w", signal, err)
		}
		defer closeIdleRelayDestination(target)
		body, err := target.marshalExport(request, routeTestSignals[signal].maxBytes)
		if err != nil {
			return nil, fmt.Errorf("encode %s test: %w", signal, err)
		}
		prepared[i] = preparedSend{target: target, body: body}
	}

	results := make([]dataexports.SignalTestResult, len(registered.Signals))
	var wg sync.WaitGroup
	for i, signal := range registered.Signals {
		wg.Go(func() {
			results[i] = sendTestRequest(ctx, signal, prepared[i].target, prepared[i].body)
		})
	}
	wg.Wait()
	return results, nil
}

// sendTestRequest posts one encoded test. Without an answer the destination is unreachable.
func sendTestRequest(ctx context.Context, signal dataexports.Signal, target *relayDestination, body []byte) dataexports.SignalTestResult {
	ctx, cancel := context.WithTimeout(ctx, routeTestSignalTimeout)
	defer cancel()
	statusCode, _ := target.sendExport(ctx, body)
	return dataexports.SignalTestResult{Signal: signal, StatusCode: statusCode, Unreachable: statusCode == 0}
}
