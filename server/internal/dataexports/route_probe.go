package dataexports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// RouteTestLimitName namespaces the per-route test allowance.
	RouteTestLimitName = "data-export-route-test"

	// RouteTestsPerMinute is how many test events one route may send a minute.
	RouteTestsPerMinute = 5

	// RouteTestOrganizationLimitName namespaces the per-organization test allowance.
	RouteTestOrganizationLimitName = "data-export-route-test-organization"

	// RouteTestsPerOrganizationPerMinute bounds an organization's tests across
	// all its routes and projects, so re-creating routes cannot multiply sends.
	RouteTestsPerOrganizationPerMinute = 20

	// routeTestTimeout bounds the whole test. A source's signals go out in
	// parallel, each with its own shorter timeout.
	routeTestTimeout = 10 * time.Second
)

// TestOutcome is what a test event got back from the destination.
type TestOutcome string

const (
	TestOutcomeAccepted    TestOutcome = "accepted"
	TestOutcomeRefused     TestOutcome = "refused"
	TestOutcomeNotFound    TestOutcome = "not_found"
	TestOutcomeUnreachable TestOutcome = "unreachable"
	TestOutcomeError       TestOutcome = "error"
)

var (
	// ErrRouteTestUnsupported means the route's data source has no test event.
	ErrRouteTestUnsupported = errors.New("this data source has no test event")

	// ErrRouteTestRateLimited means the route or its organization sent its allowance of test events.
	ErrRouteTestRateLimited = errors.New("too many test events were sent just now")

	// ErrRouteTestUnavailable means the rate limiter could not be checked, so nothing was sent.
	ErrRouteTestUnavailable = errors.New("test events cannot be sent right now")
)

// RouteTestLimits bounds test events. A test is sent only when both allow it.
type RouteTestLimits struct {
	// Route is keyed on the route ID.
	Route *ratelimit.Limiter

	// Organization is keyed on the organization ID.
	Organization *ratelimit.Limiter
}

// TestDestination is where a test event goes: one route's destination with its headers decrypted.
type TestDestination struct {
	OrganizationID       string
	ProjectID            uuid.UUID
	EndpointURL          string
	Headers              map[string]string
	IncludeSensitiveData bool
}

// SignalTestResult is what one signal's test request got back.
type SignalTestResult struct {
	Signal Signal
	// StatusCode is zero when no response came back.
	StatusCode int
	// Unreachable means no response came back: a timeout, a refused
	// connection, or an address the outbound network policy blocks.
	Unreachable bool
}

// TestEventSender sends a source's test events straight to a destination,
// through the relays' delivery path. It returns one result per signal the
// source exports; an error means the test could not be built or sent. Every log
// and span one test sends carries testEventID.
type TestEventSender interface {
	SendTest(ctx context.Context, source string, destination TestDestination, testEventID string) ([]SignalTestResult, error)
}

// RouteTestCore sends a test event for one route. The dashboard and the
// Platform MCP share it, so they share its checks, rate limit and audit entry.
type RouteTestCore struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	audit      *audit.Logger
	encryption *encryption.Client
	sender     TestEventSender
	limits     RouteTestLimits
}

// NewRouteTestCore returns nil when a dependency is missing, so callers can
// report the feature as unavailable instead of failing later.
func NewRouteTestCore(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger, encryptionClient *encryption.Client, sender TestEventSender, limits RouteTestLimits) *RouteTestCore {
	if logger == nil || db == nil || auditLogger == nil || encryptionClient == nil || sender == nil || limits.Route == nil || limits.Organization == nil {
		return nil
	}
	return &RouteTestCore{logger: logger.With(attr.SlogComponent("dataexports.route_test")), db: db, audit: auditLogger, encryption: encryptionClient, sender: sender, limits: limits}
}

// SendRouteTestParams names one route exactly. Expect, when set, must match
// the route's data source and destination, so a caller that confirmed a
// destination never tests a different one.
type SendRouteTestParams struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	RouteID          uuid.UUID
	Expect           *RouteExpectation
	Actor            urn.Principal
	ActorDisplayName *string
}

// RouteTestResult never carries header values or the destination's response body.
type RouteTestResult struct {
	Outcome       TestOutcome
	StatusCode    int
	Message       string
	Signals       []SignalTestOutcome
	TestEventID   string
	DataSource    string
	DestinationID uuid.UUID
}

// SignalTestOutcome is one signal's outcome, as shown to the admin.
type SignalTestOutcome struct {
	Signal     Signal
	Outcome    TestOutcome
	StatusCode int
	Message    string
}

// SendTest checks the route, spends one test from its allowance, sends the
// test event and records the outcome in the audit log.
func (c *RouteTestCore) SendTest(ctx context.Context, params SendRouteTestParams) (RouteTestResult, error) {
	if c == nil {
		return RouteTestResult{}, errors.New("data export route test core is not composed")
	}
	queries := repo.New(c.db)
	route, err := queries.GetDataExportRoute(ctx, repo.GetDataExportRouteParams{
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
		ID:             params.RouteID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RouteTestResult{}, ErrRouteNotFound
	}
	if err != nil {
		return RouteTestResult{}, fmt.Errorf("load data export route: %w", err)
	}
	if _, ok := LookupSource(route.DataSource); !ok {
		return RouteTestResult{}, ErrRouteTestUnsupported
	}
	if params.Expect != nil {
		changed := &RouteChangedError{
			DestinationChanged: route.OtelDestinationID != params.Expect.OtelDestinationID,
			DataSourceChanged:  route.DataSource != params.Expect.DataSource,
		}
		if changed.DestinationChanged || changed.DataSourceChanged {
			return RouteTestResult{}, changed
		}
	}
	destination, err := checkRouteDestination(ctx, queries, c.encryption, params.OrganizationID, params.ProjectID, route.OtelDestinationID, true)
	if err != nil {
		return RouteTestResult{}, err
	}

	logger := c.logger.With(attr.SlogOrganizationID(params.OrganizationID), attr.SlogProjectID(params.ProjectID.String()))
	if err := c.allow(ctx, logger, params.OrganizationID, route.ID); err != nil {
		return RouteTestResult{}, err
	}

	// One allowance covers every signal: a product telemetry test sends three requests.
	sendCtx, cancel := context.WithTimeout(ctx, routeTestTimeout)
	defer cancel()
	testEventID := uuid.NewString()
	sent, err := c.sender.SendTest(sendCtx, route.DataSource, TestDestination{
		OrganizationID:       params.OrganizationID,
		ProjectID:            params.ProjectID,
		EndpointURL:          destination.EndpointURL,
		Headers:              destination.Headers,
		IncludeSensitiveData: destination.IncludeSensitiveData,
	}, testEventID)
	if err != nil {
		return RouteTestResult{}, fmt.Errorf("send data export test event: %w", err)
	}
	if len(sent) == 0 {
		return RouteTestResult{}, errors.New("send data export test event: no signal was sent")
	}

	result := summarizeRouteTest(sent)
	result.TestEventID = testEventID
	result.DataSource = route.DataSource
	result.DestinationID = destination.ID
	auditSignals := make([]audit.DataExportRouteTestSignal, len(result.Signals))
	for i, signal := range result.Signals {
		auditSignals[i] = audit.DataExportRouteTestSignal{Signal: string(signal.Signal), Outcome: string(signal.Outcome), StatusCode: signal.StatusCode}
	}
	if err := c.audit.LogDataExportRouteTest(context.WithoutCancel(ctx), c.db, audit.LogDataExportRouteTestEvent{
		OrganizationID:   params.OrganizationID,
		ProjectID:        params.ProjectID,
		Actor:            params.Actor,
		ActorDisplayName: params.ActorDisplayName,
		ActorSlug:        nil,
		RouteURN:         urn.NewDataExportRoute(route.ID),
		DataSource:       route.DataSource,
		DestinationURN:   urn.NewOtelDestination(destination.ID),
		Outcome:          string(result.Outcome),
		StatusCode:       result.StatusCode,
		Signals:          auditSignals,
		TestEventMarker:  testEventID,
	}); err != nil {
		// The event was already sent, so the caller still gets its outcome.
		logger.ErrorContext(ctx, "audit data export route test", attr.SlogError(err))
	}
	return result, nil
}

// allow spends one test from the route's and the organization's allowances.
// It fails closed: when the limiter cannot be checked, nothing is sent.
func (c *RouteTestCore) allow(ctx context.Context, logger *slog.Logger, organizationID string, routeID uuid.UUID) error {
	for _, check := range []struct {
		limiter *ratelimit.Limiter
		key     string
	}{
		{limiter: c.limits.Route, key: routeID.String()},
		{limiter: c.limits.Organization, key: organizationID},
	} {
		result, err := check.limiter.Allow(ctx, check.key)
		if err != nil {
			logger.ErrorContext(ctx, "check data export route test rate limit", attr.SlogError(err))
			return ErrRouteTestUnavailable
		}
		if !result.Allowed {
			return ErrRouteTestRateLimited
		}
	}
	return nil
}

// testOutcomeSeverity orders outcomes from worst to best; the worst signal sets the test's outcome.
var testOutcomeSeverity = []TestOutcome{TestOutcomeUnreachable, TestOutcomeRefused, TestOutcomeNotFound, TestOutcomeError, TestOutcomeAccepted}

// summarizeRouteTest gives each signal its outcome and the test the worst of them.
func summarizeRouteTest(sent []SignalTestResult) RouteTestResult {
	signals := make([]SignalTestOutcome, len(sent))
	worst := -1
	for i, item := range sent {
		outcome, statusCode := TestOutcomeUnreachable, 0
		if !item.Unreachable {
			outcome, statusCode = testOutcomeForStatus(item.StatusCode), item.StatusCode
		}
		signals[i] = SignalTestOutcome{Signal: item.Signal, Outcome: outcome, StatusCode: statusCode, Message: testOutcomeMessage(item.Signal, outcome, statusCode)}
		if worst < 0 || slices.Index(testOutcomeSeverity, outcome) < slices.Index(testOutcomeSeverity, signals[worst].Outcome) {
			worst = i
		}
	}

	result := RouteTestResult{
		Outcome:       signals[worst].Outcome,
		StatusCode:    signals[worst].StatusCode,
		Message:       signals[worst].Message,
		Signals:       signals,
		TestEventID:   "",
		DataSource:    "",
		DestinationID: uuid.Nil,
	}
	sameOutcome := !slices.ContainsFunc(signals, func(signal SignalTestOutcome) bool { return signal.Outcome != result.Outcome })
	if len(signals) > 1 {
		names := make([]string, len(signals))
		for i, signal := range signals {
			names[i] = string(signal.Signal)
		}
		switch {
		case result.Outcome == TestOutcomeAccepted:
			result.Message = fmt.Sprintf("The destination accepted the test events for %s.", joinNames(names, "and"))
		case sameOutcome:
			result.Message = uniformOutcomeMessage(result.Outcome, signals, names)
		default:
			result.Message = "The destination did not accept every test event. See each signal for details."
		}
	}
	return result
}

// joinNames joins names as "a, b and c", or with "or".
func joinNames(names []string, conjunction string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " " + conjunction + " " + names[len(names)-1]
}

// uniformOutcomeMessage is the message when every signal failed the same way.
// It names no single signal, so it speaks for all of them.
func uniformOutcomeMessage(outcome TestOutcome, signals []SignalTestOutcome, names []string) string {
	switch outcome {
	case TestOutcomeNotFound:
		paths := make([]string, len(names))
		for i, name := range names {
			paths[i] = "/v1/" + name
		}
		return fmt.Sprintf("No endpoint at this address for %s. Check the URL; Speakeasy adds %s.", joinNames(names, "or"), joinNames(paths, "and"))
	case TestOutcomeError:
		if slices.ContainsFunc(signals, func(signal SignalTestOutcome) bool { return signal.StatusCode != signals[0].StatusCode }) {
			return "The destination returned an error for every test event. See each signal for details."
		}
		return signals[0].Message
	case TestOutcomeAccepted, TestOutcomeRefused, TestOutcomeUnreachable:
		return signals[0].Message
	default:
		return signals[0].Message
	}
}

// testOutcomeForStatus maps the destination's HTTP status to an outcome.
func testOutcomeForStatus(statusCode int) TestOutcome {
	switch {
	case statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
		return TestOutcomeAccepted
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return TestOutcomeRefused
	case statusCode == http.StatusNotFound:
		return TestOutcomeNotFound
	default:
		return TestOutcomeError
	}
}

// testOutcomeMessage is the plain-language result shown to the admin.
func testOutcomeMessage(signal Signal, outcome TestOutcome, statusCode int) string {
	switch outcome {
	case TestOutcomeAccepted:
		return "The destination accepted the test event."
	case TestOutcomeRefused:
		return "The destination refused the test event. Check the API key, and for Datadog, that the endpoint matches your site."
	case TestOutcomeNotFound:
		return fmt.Sprintf("No %s endpoint at this address. Check the URL; Speakeasy adds /v1/%s.", signal, signal)
	case TestOutcomeUnreachable:
		return "Speakeasy could not reach the destination. Check the URL, and that the address accepts connections from the internet."
	case TestOutcomeError:
		fallthrough
	default:
		return fmt.Sprintf("The destination returned an error (HTTP %d).", statusCode)
	}
}
