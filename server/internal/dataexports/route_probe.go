package dataexports

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

	// ErrRouteTestRateLimited means the route sent its allowance of test events.
	ErrRouteTestRateLimited = errors.New("too many test events for this data export route")
)

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
	db         *pgxpool.Pool
	audit      *audit.Logger
	encryption *encryption.Client
	sender     TestEventSender
	limiter    *ratelimit.Limiter
}

// NewRouteTestCore returns nil when a dependency is missing, so callers can
// report the feature as unavailable instead of failing later.
func NewRouteTestCore(db *pgxpool.Pool, auditLogger *audit.Logger, encryptionClient *encryption.Client, sender TestEventSender, limiter *ratelimit.Limiter) *RouteTestCore {
	if db == nil || auditLogger == nil || encryptionClient == nil || sender == nil || limiter == nil {
		return nil
	}
	return &RouteTestCore{db: db, audit: auditLogger, encryption: encryptionClient, sender: sender, limiter: limiter}
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

	switch allowed, err := c.limiter.Allow(ctx, route.ID.String()); {
	case err != nil:
		// A limiter outage is not a throttle; the client timeout still bounds each send.
	case !allowed.Allowed:
		return RouteTestResult{}, ErrRouteTestRateLimited
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
		outcome = TestOutcomeForStatus(statusCode)
	}

	result := RouteTestResult{
		Outcome:       outcome,
		StatusCode:    statusCode,
		Message:       TestOutcomeMessage(outcome, statusCode),
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
		return RouteTestResult{}, fmt.Errorf("audit data export route test: %w", err)
	}
	return result, nil
}

// TestOutcomeForStatus maps the destination's HTTP status to an outcome.
func TestOutcomeForStatus(statusCode int) TestOutcome {
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

// TestOutcomeMessage is the plain-language result shown to the admin.
func TestOutcomeMessage(outcome TestOutcome, statusCode int) string {
	switch outcome {
	case TestOutcomeAccepted:
		return "The destination accepted the test event."
	case TestOutcomeRefused:
		return "The destination refused the test event. Check the API key, and for Datadog, that the endpoint matches your site."
	case TestOutcomeNotFound:
		return "No logs endpoint at this address. Check the URL; Speakeasy adds /v1/logs."
	case TestOutcomeUnreachable:
		return "Speakeasy could not reach the destination."
	case TestOutcomeError:
		return fmt.Sprintf("The destination returned an error (HTTP %d).", statusCode)
	default:
		return fmt.Sprintf("The destination returned an error (HTTP %d).", statusCode)
	}
}
