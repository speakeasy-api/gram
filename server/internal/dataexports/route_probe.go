package dataexports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

	// routeTestTimeout bounds the whole send, on top of the HTTP client's own timeout.
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
	// ErrTestUnreachable means no HTTP response came back: a timeout, a refused
	// connection, or an address the outbound network policy blocks.
	ErrTestUnreachable = errors.New("the data export destination could not be reached")

	// ErrRouteTestUnsupported means the route's data source has no test event.
	ErrRouteTestUnsupported = errors.New("test events are only available for MCP connection logs exports")

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

// TestEventSender sends one test event straight to a destination, through the
// relay's delivery path. It returns the HTTP status, or an error wrapping
// ErrTestUnreachable when no response came back.
type TestEventSender interface {
	SendConnectionLogTest(ctx context.Context, destination TestDestination) (int, error)
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
	DataSource    string
	DestinationID uuid.UUID
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
	if route.DataSource != DataSourceMCPConnectionLogs {
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

	sendCtx, cancel := context.WithTimeout(ctx, routeTestTimeout)
	defer cancel()
	statusCode, err := c.sender.SendConnectionLogTest(sendCtx, TestDestination{
		OrganizationID:       params.OrganizationID,
		ProjectID:            params.ProjectID,
		EndpointURL:          destination.EndpointURL,
		Headers:              destination.Headers,
		IncludeSensitiveData: destination.IncludeSensitiveData,
	})
	var outcome TestOutcome
	switch {
	case errors.Is(err, ErrTestUnreachable):
		outcome, statusCode = TestOutcomeUnreachable, 0
	case err != nil:
		return RouteTestResult{}, fmt.Errorf("send data export test event: %w", err)
	default:
		outcome = testOutcomeForStatus(statusCode)
	}

	result := RouteTestResult{
		Outcome:       outcome,
		StatusCode:    statusCode,
		Message:       testOutcomeMessage(outcome, statusCode),
		DataSource:    route.DataSource,
		DestinationID: destination.ID,
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
		Outcome:          string(outcome),
		StatusCode:       statusCode,
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
func testOutcomeMessage(outcome TestOutcome, statusCode int) string {
	switch outcome {
	case TestOutcomeAccepted:
		return "The destination accepted the test event."
	case TestOutcomeRefused:
		return "The destination refused the test event. Check the API key, and for Datadog, that the endpoint matches your site."
	case TestOutcomeNotFound:
		return "No logs endpoint at this address. Check the URL; Speakeasy adds /v1/logs."
	case TestOutcomeUnreachable:
		return "Speakeasy could not reach the destination. Check the URL, and that the address accepts connections from the internet."
	case TestOutcomeError:
		fallthrough
	default:
		return fmt.Sprintf("The destination returned an error (HTTP %d).", statusCode)
	}
}
