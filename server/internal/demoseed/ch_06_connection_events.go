package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/connectionevents"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// connectionAttemptDays stays well inside the events table's 30-day TTL.
	connectionAttemptDays = 12

	// connectionAttemptsPerDay gives every server and result rows on most days.
	connectionAttemptsPerDay = 12

	// demoConnectionProvider and demoConnectionApp are fictional example hosts.
	demoConnectionProvider = "https://auth.provider.example.com/oauth"
	demoConnectionApp      = "https://client.example.com/oauth/metadata.json"
)

// connectionEnding is how one seeded attempt ends.
type connectionEnding struct {
	step connectionevents.Step
	what connectionevents.Happened
}

var connectionEndings = []connectionEnding{
	{connectionevents.StepInboundToken, connectionevents.ResultCodeIssued},
	{connectionevents.StepInboundToken, connectionevents.ResultCodeIssued},
	{connectionevents.StepInboundToken, connectionevents.ResultCodeIssued},
	{connectionevents.StepInboundToken, connectionevents.ResultCodeIssued},
	{connectionevents.StepInboundConsent, connectionevents.CodeCanceled},
	{connectionevents.StepInboundAuthorize, connectionevents.CodeCIMDAdmissionDenied},
	{connectionevents.StepInboundToken, connectionevents.CodeCodeNotFoundOrExpired},
	{connectionevents.StepProviderSignIn, connectionevents.CodeProviderOAuthInvalidClient},
	{connectionevents.StepProviderSignIn, connectionevents.CodeProviderRateLimited},
	{connectionevents.StepProviderSignIn, connectionevents.CodeTunnelUnreachable},
	{connectionevents.StepProviderSignIn, connectionevents.CodeCanceled},
	{connectionevents.StepInboundSignIn, connectionevents.CodeNotOrgMember},
}

// chConnectionEvents seeds connection attempts on a project server, a project
// gateway and the organization gateway. Every tenth attempt stops with no final
// row, which is how readers see an abandoned sign-in.
func (s *seeder) chConnectionEvents(ctx context.Context, ch driver.Conn) error {
	app := connectionevents.AppOf(demoConnectionApp)
	scopes := []connectionevents.Scope{
		connectionevents.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), app),
		connectionevents.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), connectionevents.AppDCR),
		connectionevents.OrganizationGatewayScope(s.orgID(), orggateway.GatewayID(s.orgID()), app),
	}

	rows := make([]connectionevents.Row, 0, connectionAttemptDays*connectionAttemptsPerDay*2)
	for day := range connectionAttemptDays {
		for n := range connectionAttemptsPerDay {
			key := "connection-attempt-" + strconv.Itoa(day) + "-" + strconv.Itoa(n)
			scope := scopes[hash64(s.name(key), "scope")%uint64(len(scopes))]
			started := s.now.AddDate(0, 0, -day-1).Add(time.Duration(hash64(s.name(key), "at")%(20*3600)) * time.Second)
			attempt := connectionevents.AttemptAt(s.det(key), started, scope)
			ending := connectionEndings[hash64(s.name(key), "ending")%uint64(len(connectionEndings))]
			abandoned := hash64(s.name(key), "abandoned")%10 == 0

			attemptRows, err := s.connectionAttemptRows(attempt, key, ending, abandoned)
			if err != nil {
				return err
			}
			rows = append(rows, attemptRows...)
		}
	}
	if err := connectionevents.AppendRows(ctx, ch, rows); err != nil {
		return fmt.Errorf("seed connection events: %w", err)
	}
	return s.chRunChecks(ctx, ch, []chCheck{
		{
			cond:     `(SELECT count() FROM mcp_connection_events WHERE organization_id = @org AND is_final) < 50`,
			settings: "",
			msg:      "demo seed postflight: expected final connection event rows",
		},
		{
			cond:     `(SELECT uniqExact(outcome) FROM mcp_connection_events WHERE organization_id = @org AND is_final) != 4`,
			settings: "",
			msg:      "demo seed postflight: connection events must cover connected, failed, blocked and declined",
		},
		{
			cond:     `(SELECT count() FROM mcp_connection_attempt_summaries WHERE organization_id = @org) = 0`,
			settings: "",
			msg:      "demo seed postflight: connection attempt rollup is empty",
		},
	})
}

// connectionAttemptRows builds one attempt: the authorize step, then its ending
// unless it was abandoned there.
func (s *seeder) connectionAttemptRows(attempt connectionevents.Attempt, key string, ending connectionEnding, abandoned bool) ([]connectionevents.Row, error) {
	first, err := connectionevents.RowAt(attempt, connectionevents.StepInboundAuthorize, connectionevents.ResultAccepted, false, attempt.StartedAt())
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	first.TraceID = md5Hex(s.name(key + "-authorize"))
	if abandoned {
		return []connectionevents.Row{first}, nil
	}

	subject := urn.NewUserSubject(s.userID("amara"))
	opts := []connectionevents.Option{connectionevents.Subject(&subject)}
	if ending.step == connectionevents.StepProviderSignIn {
		opts = append(opts, connectionevents.Provider(demoConnectionProvider))
	}
	if ending.what == connectionevents.ResultCodeIssued {
		opts = append(opts, connectionevents.Grant("authorization_code"))
	}
	last, err := connectionevents.RowAt(attempt, ending.step, ending.what, true, attempt.StartedAt().Add(45*time.Second), opts...)
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	last.TraceID = md5Hex(s.name(key + "-ending"))
	return []connectionevents.Row{first, last}, nil
}
