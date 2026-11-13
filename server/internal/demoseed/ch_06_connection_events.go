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
	// ConnectionAttemptDays stays well inside the events table's 30-day TTL.
	ConnectionAttemptDays = 12

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

	// onceOneIn makes one attempt in this many a single refused MCP request, as Once records it.
	onceOneIn = 6

	// repeatedOneIn makes one such request in this many stand for repeated ones.
	repeatedOneIn = 2

	// maxSeededRepeats is the most held-back rows one seeded row stands for.
	maxSeededRepeats = 40

	// demoConnectionProvider is a fictional host. demoConnectionApp is a catalog app, since only those keep their host.
	demoConnectionProvider = "https://auth.provider.example.com/oauth"
	demoConnectionApp      = "https://claude.ai/oauth/claude-code-client-metadata"
)

// connectionEnding is how one seeded attempt ends.
type connectionEnding struct {
	step mcpconnectionattempts.Step
	what mcpconnectionattempts.Happened
}

// demoScriptVersion is the seeded HTTP library version, as its User-Agent names it.
const demoScriptVersion = "0.28.1"

// connectionClients is the clients seeded attempts come from.
var connectionClients = []struct{ family, version string }{
	{mcpconnectionattempts.FamilyCodexCLI, "0.77.0"},
	{mcpconnectionattempts.FamilyClaudeCode, "2.0.14"},
	{mcpconnectionattempts.FamilyCursor, "2.0.77"},
	{mcpconnectionattempts.FamilyVSCode, "1.105.1"},
	{mcpconnectionattempts.FamilyOpenAIConnector, "1.0.0"},
	{mcpconnectionattempts.FamilyScript, demoScriptVersion},
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
	{mcpconnectionattempts.StepInboundSignIn, mcpconnectionattempts.CodeProviderDenied},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodePKCEMismatch},
	{mcpconnectionattempts.StepProviderToken, mcpconnectionattempts.CodeProviderOAuthInvalidGrant},
}

// connectionRequestEndings are the single-row outcomes: a refused or failed
// request, and a refresh. A served request has no row.
var connectionRequestEndings = []connectionEnding{
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidRemoteSession},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStoredSignInUnreadable},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeNoServerAccess},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeUsageLimitReached},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeRemoteSessionUnavailable},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeToolPermissionDenied},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeMCPToolCallsPaused},
	{mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderServerError},
	{mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.ResultRefreshed},
	{mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.ResultRefreshed},
	{mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeInvalidGrant},
	{mcpconnectionattempts.StepProviderRefresh, mcpconnectionattempts.CodeProviderServerError},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshed},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshReplayed},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenRevoked},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUsedByOtherApp},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenClientMismatch},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeStoredSessionInvalid},
	{mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSignInRevokedByAdmin},
	{mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.ResultRenewed},
	{mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeReauthenticationRequired},
	{mcpconnectionattempts.StepSignInRenewal, mcpconnectionattempts.CodeRenewalRetryable},
}

// connectionRequestRows seeds single-row outcomes each day, each its own attempt.
func (s *seeder) connectionRequestRows() ([]mcpconnectionattempts.Row, error) {
	scopes := s.connectionScopes()
	subject := urn.NewUserSubject(s.userID("amara"))
	rows := make([]mcpconnectionattempts.Row, 0, ConnectionAttemptDays*len(connectionRequestEndings))
	for day := range ConnectionAttemptDays {
		for n, ending := range connectionRequestEndings {
			key := "connection-request-" + strconv.Itoa(day) + "-" + strconv.Itoa(n)
			scope := scopes[hash64(s.name(key), "scope")%uint64(len(scopes))]
			// Half the refreshes are the scheduled sweep, which reaches no server.
			scheduled := ending.step == mcpconnectionattempts.StepProviderRefresh && hash64(s.name(key), "scheduled")%2 == 0
			switch {
			case ending.step == mcpconnectionattempts.StepSignInRenewal:
				scope = mcpconnectionattempts.IdentityProviderScope(s.orgID(), mcpconnectionattempts.AppUnverified)
			case scheduled:
				scope = mcpconnectionattempts.ProviderSessionScope(s.orgID(), s.projectID(), mcpconnectionattempts.AppBackground)
			}
			started := s.now.AddDate(0, 0, -day-1).Add(time.Duration(hash64(s.name(key), "at")%connectionAttemptWindowSeconds) * time.Second)
			opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&subject)}
			// On a gateway, these refusals come from a selected member, never the gateway itself.
			if scope != s.connectionServerScope() && ending.step == mcpconnectionattempts.StepMCPRequest && (ending.what == mcpconnectionattempts.CodeProviderServerError || ending.what == mcpconnectionattempts.CodeToolPermissionDenied || ending.what == mcpconnectionattempts.CodeUsageLimitReached || ending.what == mcpconnectionattempts.CodeProviderRejected || ending.what == mcpconnectionattempts.CodeMCPToolCallsPaused || ending.what == mcpconnectionattempts.CodeStoredSignInUnreadable) {
				scope = mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR)
				opts = append(opts, mcpconnectionattempts.Member(s.det("mcpserver-support")))
			}
			switch ending.step {
			case mcpconnectionattempts.StepProviderRefresh:
				opts = append(opts, mcpconnectionattempts.Provider(demoConnectionProvider))
			case mcpconnectionattempts.StepInboundToken:
				opts = append(opts, mcpconnectionattempts.Grant(oauthwire.GrantTypeRefreshToken))
			}
			row, err := mcpconnectionattempts.RowAt(mcpconnectionattempts.AttemptAt(s.det(key), started, scope), ending.step, ending.what, true, started, opts...)
			if err != nil {
				return nil, fmt.Errorf("build connection request event: %w", err)
			}
			row.TraceID = md5Hex(s.name(key))
			if ending.step == mcpconnectionattempts.StepMCPRequest || ending.step == mcpconnectionattempts.StepInboundToken || (ending.step == mcpconnectionattempts.StepProviderRefresh && !scheduled) {
				s.setConnectionClient(&row, key)
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// chConnectionEvents seeds connection attempts on a project server, a project
// gateway and the organization gateway. Every tenth attempt stops with no final
// row, which is how readers see an abandoned sign-in.
func (s *seeder) chConnectionEvents(ctx context.Context, ch driver.Conn) error {
	scopes := s.connectionScopes()

	rows := make([]mcpconnectionattempts.Row, 0, ConnectionAttemptDays*connectionAttemptsPerDay*2)
	for day := range ConnectionAttemptDays {
		for n := range connectionAttemptsPerDay {
			key := "connection-attempt-" + strconv.Itoa(day) + "-" + strconv.Itoa(n)
			scope := scopes[hash64(s.name(key), "scope")%uint64(len(scopes))]
			started := s.now.AddDate(0, 0, -day-1).Add(time.Duration(hash64(s.name(key), "at")%connectionAttemptWindowSeconds) * time.Second)
			attempt := mcpconnectionattempts.AttemptAt(s.det(key), started, scope)
			if hash64(s.name(key), "once")%onceOneIn == 0 {
				row, err := s.connectionOnceRow(attempt, key)
				if err != nil {
					return err
				}
				rows = append(rows, row)
				continue
			}
			ending := connectionEndings[hash64(s.name(key), "ending")%uint64(len(connectionEndings))]
			abandoned := hash64(s.name(key), "abandoned")%abandonedOneIn == 0

			attemptRows, err := s.connectionAttemptRows(attempt, key, ending, abandoned)
			if err != nil {
				return err
			}
			rows = append(rows, attemptRows...)
		}
	}
	requestRows, err := s.connectionRequestRows()
	if err != nil {
		return err
	}
	rows = append(rows, requestRows...)
	tokenRows, err := s.connectionWorkloadRows()
	if err != nil {
		return err
	}
	rows = append(rows, tokenRows...)
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
		{
			cond:     `(SELECT countIf(client_family = 'browser') FROM mcp_connection_events WHERE organization_id = @org) = 0 OR (SELECT countIf(client_family != 'browser' AND step NOT IN ('inbound_token', 'mcp_request', 'provider_refresh', 'sign_in_renewal')) FROM mcp_connection_events WHERE organization_id = @org) > 0 OR (SELECT countIf(repeats > 0 AND NOT (is_final AND step = 'mcp_request')) FROM mcp_connection_events WHERE organization_id = @org) > 0`,
			settings: "",
			msg:      "demo seed postflight: sign-in rows come from a browser, and only one-row requests carry repeats",
		},
	})
}

// connectionOnceRow is one refused MCP request. Only these rows carry repeats, as Once
// holds back a repeating key and counts it on the next row.
func (s *seeder) connectionOnceRow(attempt mcpconnectionattempts.Attempt, key string) (mcpconnectionattempts.Row, error) {
	row, err := mcpconnectionattempts.RowAt(attempt, mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken, true, attempt.StartedAt())
	if err != nil {
		return row, fmt.Errorf("build connection event: %w", err)
	}
	row.TraceID = md5Hex(s.name(key + "-once"))
	s.setConnectionClient(&row, key)
	if hash64(s.name(key), "repeats")%repeatedOneIn == 0 {
		row.Repeats = uint32(hash64(s.name(key), "repeat-count")%maxSeededRepeats) + 1
	}
	return row, nil
}

// connectionScopes is where seeded attempts happen: a project server, a project gateway and the organization gateway.
func (s *seeder) connectionScopes() []mcpconnectionattempts.Scope {
	return []mcpconnectionattempts.Scope{
		s.connectionServerScope(),
		mcpconnectionattempts.GatewayScope(s.orgID(), s.projectID(), s.det("metamcp-1"), mcpconnectionattempts.AppDCR),
		mcpconnectionattempts.OrganizationGatewayScope(s.orgID(), orggateway.GatewayID(s.orgID()), mcpconnectionattempts.AppOf(demoConnectionApp, mcpconnectionattempts.ClientIDAdmitted)),
	}
}

func (s *seeder) connectionServerScope() mcpconnectionattempts.Scope {
	return mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppOf(demoConnectionApp, mcpconnectionattempts.ClientIDAdmitted))
}

func (s *seeder) setConnectionClient(row *mcpconnectionattempts.Row, key string) {
	client := connectionClients[hash64(s.name(key), "client")%uint64(len(connectionClients))]
	row.ClientFamily, row.ClientVersion = client.family, client.version
	row.DeviceKey = s.connectionDevice(key)
}

func (s *seeder) connectionDevice(key string) string {
	return md5Hex(s.name("connection-device-" + strconv.FormatUint(hash64(s.name(key), "device")%seededDevices, 10)))
}

// connectionAttemptRows builds one attempt: the authorize step, then its ending
// unless it was abandoned there. Sign-in pages are opened in a browser, and only
// the token step comes from the client itself.
func (s *seeder) connectionAttemptRows(attempt mcpconnectionattempts.Attempt, key string, ending connectionEnding, abandoned bool) ([]mcpconnectionattempts.Row, error) {
	first, err := mcpconnectionattempts.RowAt(attempt, mcpconnectionattempts.StepInboundAuthorize, mcpconnectionattempts.ResultAccepted, false, attempt.StartedAt())
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	first.TraceID = md5Hex(s.name(key + "-authorize"))
	first.ClientFamily = mcpconnectionattempts.FamilyBrowser
	first.DeviceKey = s.connectionDevice(key)
	if abandoned {
		return s.stoppedAttemptRows(attempt, key, first)
	}

	rows := []mcpconnectionattempts.Row{first}
	if ending.step == mcpconnectionattempts.StepInboundAuthorize {
		// A refused authorize was never accepted first.
		rows = nil
	}

	subject := urn.NewUserSubject(s.userID("amara"))
	opts := []mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&subject)}
	if ending.step == mcpconnectionattempts.StepProviderSignIn || ending.step == mcpconnectionattempts.StepProviderToken {
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
	last.ClientFamily, last.DeviceKey = mcpconnectionattempts.FamilyBrowser, first.DeviceKey
	if ending.step == mcpconnectionattempts.StepInboundToken {
		s.setConnectionClient(&last, key)
	}
	return append(rows, last), nil
}

// stoppedAttemptRows stops on a failed provider sign-in; project gateways also show an active member.
func (s *seeder) stoppedAttemptRows(attempt mcpconnectionattempts.Attempt, key string, first mcpconnectionattempts.Row) ([]mcpconnectionattempts.Row, error) {
	rows := []mcpconnectionattempts.Row{first}
	at := attempt.StartedAt().Add(connectionAttemptLength)
	failed, err := mcpconnectionattempts.RowAt(attempt, mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderOAuthInvalidClient, false, at, mcpconnectionattempts.Provider(demoConnectionProvider))
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	failed.TraceID = md5Hex(s.name(key + "-failed-step"))
	failed.ClientFamily, failed.DeviceKey = first.ClientFamily, first.DeviceKey
	rows = append(rows, failed)
	if first.ServerID != s.det("metamcp-1").String() {
		return rows, nil
	}
	member, err := mcpconnectionattempts.RowAt(attempt, mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeNeedsSignIn, false, at, mcpconnectionattempts.Member(s.det("mcpserver-support")))
	if err != nil {
		return nil, fmt.Errorf("build connection event: %w", err)
	}
	member.TraceID = md5Hex(s.name(key + "-member"))
	member.ClientFamily, member.DeviceKey = first.ClientFamily, first.DeviceKey
	return append(rows, member), nil
}

// connectionWorkloadRows seeds token requests from a workload and an assistant, and a server
// that rejected the credential on initialize, one of each on the last few days.
func (s *seeder) connectionWorkloadRows() ([]mcpconnectionattempts.Row, error) {
	admitted := urn.NewWorkloadSubject(s.det("workload-issuer-1"), "repo:acme/payments-api:ref:refs/heads/main")
	stranger := urn.NewWorkloadSubject(s.det("workload-issuer-1"), "repo:acme/sandbox:ref:refs/heads/experiment")
	person := urn.NewUserSubject(s.userID("amara"))
	workload := []mcpconnectionattempts.Option{mcpconnectionattempts.Grant(oauthwire.GrantTypeJWTBearer), mcpconnectionattempts.UserSessionIssuer(s.usIssuer())}
	server := mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppWorkload)
	sharedServer := mcpconnectionattempts.AuthorizationServerScope(s.orgID(), s.projectID(), s.usIssuer(), mcpconnectionattempts.AppWorkload)
	assistant := mcpconnectionattempts.ServerScope(s.orgID(), s.projectID(), s.det("mcpserver-github"), mcpconnectionattempts.AppDCR)
	seeded := []struct {
		scope  mcpconnectionattempts.Scope
		step   mcpconnectionattempts.Step
		what   mcpconnectionattempts.Happened
		caller string
		opts   []mcpconnectionattempts.Option
	}{
		{server, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultSignedIn, "workload", append([]mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&admitted)}, workload...)},
		{sharedServer, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSubjectNotAdmitted, "workload", append([]mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&stranger), mcpconnectionattempts.Detail("subject_not_admitted")}, workload...)},
		{assistant, mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSignInMethodNotAccepted, "assistant", []mcpconnectionattempts.Option{mcpconnectionattempts.Grant(oauthwire.GrantTypeClientCredentials), mcpconnectionattempts.Detail("assistant_tokens_not_issued")}},
		{s.connectionServerScope(), mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected, "", []mcpconnectionattempts.Option{mcpconnectionattempts.Subject(&person), mcpconnectionattempts.RequestMethod("initialize"), mcpconnectionattempts.Provider(demoConnectionProvider)}},
	}
	rows := make([]mcpconnectionattempts.Row, 0, len(seeded))
	for n, row := range seeded {
		key := "connection-token-" + strconv.Itoa(n)
		started := s.now.AddDate(0, 0, -n-1).Add(time.Duration(hash64(s.name(key), "at")%connectionAttemptWindowSeconds) * time.Second)
		built, err := mcpconnectionattempts.RowAt(mcpconnectionattempts.AttemptAt(s.det(key), started, row.scope), row.step, row.what, true, started, row.opts...)
		if err != nil {
			return nil, fmt.Errorf("build connection token event: %w", err)
		}
		built.TraceID = md5Hex(s.name(key))
		switch row.step {
		case mcpconnectionattempts.StepMCPRequest:
			s.setConnectionClient(&built, key)
		case mcpconnectionattempts.StepInboundToken:
			// A workload or assistant asks with an HTTP library from a machine of its own, not a person's device.
			built.ClientFamily, built.ClientVersion = mcpconnectionattempts.FamilyScript, demoScriptVersion
			built.DeviceKey = md5Hex(s.name("connection-device-" + row.caller))
		}
		rows = append(rows, built)
	}
	return rows, nil
}
