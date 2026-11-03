package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// connectionAttemptDays stays well inside the events table's 30-day TTL.
	connectionAttemptDays = 12

	// connectionAttemptsPerDay gives every server and result rows on most days.
	connectionAttemptsPerDay = 12

	// connectionAttemptWindowSeconds spreads each day's attempts across 20 hours of it.
	connectionAttemptWindowSeconds = 20 * 60 * 60

	// connectionAttemptLength is how long a seeded attempt takes to reach its end.
	connectionAttemptLength = 45 * time.Second

	// abandonedOneIn makes one attempt in this many stop with no final row.
	abandonedOneIn = 10

	// seededDevices is how many devices seeded attempts come from.
	seededDevices = 6

	// repeatedOneIn makes one failed attempt in this many stand for repeated failures.
	repeatedOneIn = 4

	// maxSeededRepeats is the most held-back rows one seeded row stands for.
	maxSeededRepeats = 40

	// demoConnectionProvider and demoConnectionApp are fictional example hosts.
	demoConnectionProvider = "https://auth.provider.example.com/oauth"
	demoConnectionApp      = "https://client.example.com/oauth/metadata.json"
)

// connectionEnding is how one seeded attempt ends.
type connectionEnding struct {
	step mcpconnectionattempts.Step
	what mcpconnectionattempts.Happened
}

// connectionClients is the clients seeded attempts come from.
var connectionClients = []struct{ family, version string }{
	{mcpconnectionattempts.FamilyCodexCLI, "0.77.0"},
	{mcpconnectionattempts.FamilyClaudeCode, "2.0.14"},
	{mcpconnectionattempts.FamilyCursor, "2.0.77"},
	{mcpconnectionattempts.FamilyVSCode, "1.105.1"},
	{mcpconnectionattempts.FamilyOpenAIConnector, "1.0.0"},
	{mcpconnectionattempts.FamilyScript, "0.28.1"},
}

var connectionEndings = []connectionEnding{
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultCodeIssued},
	{mcpconnectionattempts.StepInboundConsent, mcpconnectionattempts.CodeCanceled},
	{mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.CodeCIMDAdmissionDenied},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeCodeNotFoundOrExpired},
	{mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidClient},
	{mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderRateLimited},
	{mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeTunnelUnavailable},
	{mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied},
	{mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeNotOrgMember},
}

// chConnectionEvents seeds connection attempts on a project server, a project
// gateway and the organization gateway. Every tenth attempt stops with no final
// row, which is how readers see an abandoned sign-in.
func (s *seeder) chConnectionEvents(ctx context.Context, ch driver.Conn) error {
	app := mcpconnectionattempts.AppOf(demoConnectionApp)
	scopes := []mcpconnectionattempts.Scope{
		mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), app),
		mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR),
		mcpconnectionattempts.OrganizationGatewayScope(s.orgID(), orggateway.GatewayID(s.orgID()), app),
	}

	rows := make([]mcpconnectionattempts.Row, 0, connectionAttemptDays*connectionAttemptsPerDay*2)
	for day := range connectionAttemptDays {
		for n := range connectionAttemptsPerDay {
			key := "connection-attempt-" + strconv.Itoa(day) + "-" + strconv.Itoa(n)
			scope := scopes[hash64(s.name(key), "scope")%uint64(len(scopes))]
			started := s.now.AddDate(0, 0, -day-1).Add(time.Duration(hash64(s.name(key), "at")%connectionAttemptWindowSeconds) * time.Second)
			attempt := mcpconnectionattempts.AttemptAt(s.det(key), started, scope)
			ending := connectionEndings[hash64(s.name(key), "ending")%uint64(len(connectionEndings))]
			abandoned := hash64(s.name(key), "abandoned")%abandonedOneIn == 0

			attemptRows, err := s.connectionAttemptRows(attempt, key, ending, abandoned)
			if err != nil {
				return err
			}
			rows = append(rows, attemptRows...)
		}
	}
	if err := mcpconnectionattempts.AppendRows(ctx, ch, rows); err != nil {
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
		{
			cond:     `(SELECT uniqExact(client_family) FROM mcp_connection_attempt_summaries WHERE organization_id = @org) < 2 OR (SELECT sum(repeats) FROM mcp_connection_attempt_summaries WHERE organization_id = @org) = 0`,
			settings: "",
			msg:      "demo seed postflight: connection attempts must cover several clients and some repeats",
		},
	})
}

// connectionAttemptRows builds one attempt: the authorize step, then its ending
// unless it was abandoned there.
func (s *seeder) connectionAttemptRows(attempt mcpconnectionattempts.Attempt, key string, ending connectionEnding, abandoned bool) ([]mcpconnectionattempts.Row, error) {
	first, err := mcpconnectionattempts.RowAt(attempt, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, attempt.StartedAt())
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	first.TraceID = md5Hex(s.name(key + "-authorize"))
	client := connectionClients[hash64(s.name(key), "client")%uint64(len(connectionClients))]
	first.ClientFamily, first.ClientVersion = client.family, client.version
	first.DeviceKey = md5Hex(s.name("connection-device-" + strconv.FormatUint(hash64(s.name(key), "device")%seededDevices, 10)))
	if abandoned {
		return []mcpconnectionattempts.Row{first}, nil
	}

	rows := []mcpconnectionattempts.Row{first}
	if ending.step == mcpconnectionattempts.StepInboundAuthorize {
		// A refused authorize was never accepted first.
		rows = nil
	}

	subject := urn.NewUserSubject(s.userID("amara"))
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&subject)}
	if ending.step == mcpconnectionattempts.StepProviderSignIn {
		opts = append(opts, mcpconnectionattempts.Provider(demoConnectionProvider))
	}
	if ending.what == mcpconnectionattempts.ResultCodeIssued {
		opts = append(opts, mcpconnectionattempts.Grant(oauthwire.GrantTypeAuthorizationCode))
	}
	last, err := mcpconnectionattempts.RowAt(attempt, ending.step, ending.what, true, attempt.StartedAt().Add(connectionAttemptLength), opts...)
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	last.TraceID = md5Hex(s.name(key + "-ending"))
	last.ClientFamily, last.ClientVersion, last.DeviceKey = client.family, client.version, first.DeviceKey
	if ending.what != mcpconnectionattempts.ResultCodeIssued && hash64(s.name(key), "repeats")%repeatedOneIn == 0 {
		last.Repeats = uint32(hash64(s.name(key), "repeat-count")%maxSeededRepeats) + 1
	}
	return append(rows, last), nil
}
