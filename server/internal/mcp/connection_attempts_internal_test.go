package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestMemberAttemptForClassifiesWhatAListingRecords(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "linear"}
	discovery := func(reason attemptReason, status int) error {
		return memberDiscoveryError{metaMemberError: &metaMemberError{message: "upstream text", reason: reason}, status: status, retryable: false, retryAfter: 0}
	}
	type want struct {
		outcome attemptOutcome
		class   failureClass
		reason  attemptReason
		status  int
	}
	for name, tc := range map[string]struct {
		result   memberDescribeResult
		recorded bool
		want     want
	}{
		"listed after a retry":     {result: memberDescribeResult{catalog: &memberCatalog{}, err: nil, calls: 2}, recorded: true, want: want{outcomeListed, classNone, reasonNone, 0}},
		"incomplete catalog":       {result: memberDescribeResult{catalog: &memberCatalog{incomplete: true}, err: nil, calls: 1}, recorded: true, want: want{outcomeFailed, classEndpoint, reasonIncompleteCatalog, 0}},
		"rate limited":             {result: memberDescribeResult{catalog: nil, err: discovery(reasonRateLimited, http.StatusTooManyRequests), calls: 3}, recorded: true, want: want{outcomeFailed, classEndpoint, reasonRateLimited, http.StatusTooManyRequests}},
		"budget before a slot":     {result: memberDescribeResult{catalog: nil, err: discovery(reasonListingBudgetExhausted, 0), calls: 0}, recorded: true, want: want{outcomeFailed, classInternal, reasonListingBudgetExhausted, 0}},
		"credential load":          {result: memberDescribeResult{catalog: nil, err: discovery(reasonCredentialLoadFailed, 0), calls: 1}, recorded: true, want: want{outcomeFailed, classInternal, reasonCredentialLoadFailed, 0}},
		"access check timeout":     {result: memberDescribeResult{catalog: nil, err: discovery(reasonConnectionCheckFailed, 0), calls: 1}, recorded: true, want: want{outcomeFailed, classInternal, reasonConnectionCheckFailed, 0}},
		"discovery without reason": {result: memberDescribeResult{catalog: nil, err: discovery(reasonNone, 0), calls: 1}, recorded: true, want: want{outcomeFailed, classInternal, reasonUnclassified, 0}},
		"needs sign in":            {result: memberDescribeResult{catalog: nil, err: memberCredentialError{&metaMemberError{message: "connect it", reason: reasonNeedsSignIn}}, calls: 1}, recorded: true, want: want{outcomeCredentialBlocked, classConfiguration, reasonNeedsSignIn, 0}},
		"reauthentication":         {result: memberDescribeResult{catalog: nil, err: &metaMemberError{message: "sign in again", reason: reasonReauthenticationRequired}, calls: 1}, recorded: true, want: want{outcomeCredentialBlocked, classConfiguration, reasonReauthenticationRequired, 0}},
		"ambiguous credential":     {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("%w: %w", errAmbiguousMemberCredential, &metaMemberError{message: "duplicates", reason: reasonCredentialAmbiguous}), calls: 1}, recorded: true, want: want{outcomeCredentialBlocked, classConfiguration, reasonCredentialAmbiguous, 0}},
		"consent changed":          {result: memberDescribeResult{catalog: nil, err: &metaMemberError{message: "consent again", reason: reasonConsentConfigChanged}, calls: 1}, recorded: true, want: want{outcomeConsentBlocked, classConfiguration, reasonConsentConfigChanged, 0}},
		"not servable":             {result: memberDescribeResult{catalog: nil, err: &metaMemberError{message: "not servable", reason: reasonNotServable}, calls: 1}, recorded: false},
		"plain member error":       {result: memberDescribeResult{catalog: nil, err: &metaMemberError{message: "unknown", reason: reasonNone}, calls: 1}, recorded: false},
		"not found":                {result: memberDescribeResult{catalog: nil, err: oops.E(oops.CodeNotFound, nil, "not found"), calls: 1}, recorded: false},
		"wrapped forbidden":        {result: memberDescribeResult{catalog: nil, err: fmt.Errorf("describe: %w", oops.E(oops.CodeForbidden, nil, "forbidden")), calls: 1}, recorded: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attempt, recorded := memberAttemptFor(member, tc.result)
			require.Equal(t, tc.recorded, recorded)
			if !recorded {
				return
			}
			require.Equal(t, tc.want, want{attempt.outcome, attempt.class, attempt.reason, attempt.status})
			require.Equal(t, tc.result.calls, attempt.calls)
		})
	}
}

func TestMemberClientFailureReasonsComeFromTheFailureNotItsText(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "linear"}
	outage, ok := errors.AsType[memberDiscoveryError](metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialUnavailable))
	require.True(t, ok)
	require.Equal(t, reasonAuthorizationServerUnavailable, outage.reason)
	misconfigured, ok := errors.AsType[*metaMemberError](metaMemberClientCredentialError(member, remotesessions.ErrClientCredentialMisconfigured))
	require.True(t, ok)
	require.Equal(t, reasonCredentialMisconfigured, misconfigured.reason)
	rejected, ok := errors.AsType[memberCredentialError](memberAuthFailure(member, memberDial{build: nil, anonymous: false, clientCredential: false, connectURL: "https://example.test/connect"}))
	require.True(t, ok)
	require.Equal(t, reasonCredentialRejected, rejected.reason)
}

func TestListingResultSummarizesRecordedMembers(t *testing.T) {
	t.Parallel()

	listed := memberAttempt{member: metaMember{}, outcome: outcomeListed, class: classNone, reason: reasonNone, calls: 1, status: 0}
	endpoint := memberAttempt{member: metaMember{}, outcome: outcomeFailed, class: classEndpoint, reason: reasonRateLimited, calls: 3, status: 429}
	budget := memberAttempt{member: metaMember{}, outcome: outcomeFailed, class: classInternal, reason: reasonListingBudgetExhausted, calls: 0, status: 0}
	consent := unapprovedMemberAttempt(metaMember{})

	require.Equal(t, listingAttempt{outcome: outcomeComplete, class: classNone, reason: reasonNone}, listingResult(nil))
	require.Equal(t, listingAttempt{outcome: outcomeComplete, class: classNone, reason: reasonNone}, listingResult([]memberAttempt{listed, listed}))
	require.Equal(t, listingAttempt{outcome: outcomePartial, class: classNone, reason: reasonNone}, listingResult([]memberAttempt{listed, endpoint, consent}))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classEndpoint, reason: reasonNoMemberListed}, listingResult([]memberAttempt{budget, consent, endpoint}))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classInternal, reason: reasonNoMemberListed}, listingResult([]memberAttempt{consent, budget}))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classConfiguration, reason: reasonNoMemberListed}, listingResult([]memberAttempt{consent}))
}

func TestListingFailuresNameTheirScope(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classConfiguration, reason: reasonAmbiguousToolName}, inventoryFailure(ctx, oops.E(oops.CodeConflict, nil, "ambiguous")))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classConfiguration, reason: reasonInventoryTooLarge}, inventoryFailure(ctx, oops.E(oops.CodeRequestTooLarge, nil, "too many")))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classInternal, reason: reasonInventoryError}, inventoryFailure(ctx, oops.E(oops.CodeUnavailable, nil, "incomplete")))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classConfiguration, reason: reasonInventoryTooLarge}, encodingFailure(ctx, oops.E(oops.CodeRequestTooLarge, nil, "16 MiB")))
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classInternal, reason: reasonEncodingFailed}, encodingFailure(ctx, errors.New("marshal")))

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Equal(t, listingAttempt{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}, inventoryFailure(canceled, oops.E(oops.CodeConflict, nil, "ambiguous")))
}

func TestAttemptClientKeyNamesTheRegisteredClient(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_attempt_client_key")
	require.NoError(t, err)
	fixtures := authclientbackfill.New(pool)
	orgID, projectID := "org_"+uuid.NewString(), uuid.New()
	require.NoError(t, fixtures.SeedOrganizationFixture(ctx, orgID))
	require.NoError(t, fixtures.SeedProjectFixture(ctx, authclientbackfill.SeedProjectFixtureParams{ID: projectID, Slug: "p-" + projectID.String()[:8], OrganizationID: orgID}))
	issuers := usersessionsrepo.New(pool)
	issuer := func(slug string) uuid.UUID {
		row, err := issuers.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
			ProjectID:               projectID,
			OrganizationID:          pgtype.Text{String: "", Valid: false},
			Slug:                    slug + "-" + uuid.NewString()[:8],
			AuthnChallengeMode:      "interactive",
			SessionDuration:         pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
			AuthorizationServerMode: pgtype.Text{String: "", Valid: false},
			UseAuthenticationHost:   pgtype.Bool{Bool: false, Valid: false},
		})
		require.NoError(t, err)
		return row.ID
	}
	gatewayIssuer, otherIssuer := issuer("gateway"), issuer("other")
	const metadataURL = "https://client.example.test/oauth/metadata.json"
	client, err := issuers.UpsertUserSessionClientFromCIMD(ctx, usersessionsrepo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:     gatewayIssuer,
		ClientID:                metadataURL,
		ClientName:              "Example client",
		RedirectUris:            []string{"http://127.0.0.1/callback"},
		CacheTtlSeconds:         3600,
		ClientIDMetadataEtag:    conv.ToPGTextEmpty(""),
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	s := &Service{db: pool, logger: testenv.NewLogger(t)}
	caller := func(issuer uuid.UUID, clientID string, subject urn.SessionSubject) attemptCaller {
		return attemptCaller{projectID: projectID, organizationID: orgID, gatewayID: uuid.New(), userSessionIssuerID: issuer, subject: subject, oauthClientID: clientID, traceID: ""}
	}
	user := urn.NewUserSubject("user_example")
	require.Equal(t, client.ID.String(), s.attemptClientKey(ctx, caller(gatewayIssuer, metadataURL, user)))
	require.Equal(t, "unregistered_client", s.attemptClientKey(ctx, caller(otherIssuer, metadataURL, user)), "a client registered under another issuer is not this caller's")
	require.Equal(t, "credential:user", s.attemptClientKey(ctx, caller(gatewayIssuer, "", user)))
	require.Equal(t, "anonymous", s.attemptClientKey(ctx, caller(uuid.Nil, "", urn.SessionSubject{})))
}

func enabledForAnyOrganization(context.Context, string) (bool, error) { return true, nil }

// One listing written through the real logger: a raw row per attempt, the
// rollup counting each once, the Activity drill-in finding only the failed
// member, and no trace, tool call, or chat request.
func TestWriteDirectListingCommitsOneFactPerAttempt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	chConn, err := TestInfra.NewClickhouseClient(t)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	s := &Service{logger: logger, telemLogger: tm.NewLogger(ctx, logger, nil, nil, chConn, enabledForAnyOrganization, enabledForAnyOrganization, nil, tm.NewNoopLogPublisher(logger))}
	projectID, gatewayID, provider := uuid.New(), uuid.New(), uuid.New()
	linear := metaMember{serverID: uuid.New(), slug: "linear", remoteSessionIssuerID: uuid.NullUUID{UUID: provider, Valid: true}}
	support := metaMember{serverID: uuid.New(), slug: "support"}
	slack := metaMember{serverID: uuid.New(), slug: "slack"}
	subject := urn.NewUserSubject("user_example")
	caller := attemptCaller{projectID: projectID, organizationID: "org_" + uuid.NewString(), gatewayID: gatewayID, userSessionIssuerID: uuid.Nil, subject: subject, oauthClientID: "", traceID: "0af7651916cd43dd8448eb211c80319c"}
	attempts := []memberAttempt{
		{member: support, outcome: outcomeListed, class: classNone, reason: reasonNone, calls: 2, status: 0},
		{member: linear, outcome: outcomeFailed, class: classEndpoint, reason: reasonRateLimited, calls: 3, status: http.StatusTooManyRequests},
		{member: slack, outcome: outcomeCredentialBlocked, class: classConfiguration, reason: reasonNeedsSignIn, calls: 1, status: 0},
	}
	observedAt := time.Now()
	s.writeDirectListing(ctx, caller, observedAt, listingResult(attempts), attempts)

	type row struct {
		Stage     string `ch:"stage"`
		Outcome   string `ch:"outcome"`
		Reason    string `ch:"reason"`
		MemberID  string `ch:"mcp_server_id"`
		Subject   string `ch:"subject"`
		ClientKey string `ch:"client_key"`
		Provider  string `ch:"provider_id"`
		Retries   string `ch:"retries"`
		TraceAttr string `ch:"trace_attr"`
		Source    string `ch:"event_source"`
		ToolURN   string `ch:"gram_urn"`
		TraceID   string `ch:"trace_id"`
	}
	var rows []row
	require.NoError(t, chConn.Select(ctx, &rows,
		`SELECT toString(attributes.gram.connection_attempt.stage) AS stage,
		        toString(attributes.gram.connection_attempt.outcome) AS outcome,
		        toString(attributes.gram.connection_attempt.reason) AS reason,
		        mcp_server_id,
		        toString(attributes.gram.connection_attempt.subject) AS subject,
		        toString(attributes.gram.connection_attempt.client_key) AS client_key,
		        toString(attributes.gram.connection_attempt.provider_id) AS provider_id,
		        toString(attributes.gram.connection_attempt.retries) AS retries,
		        toString(attributes.gram.connection_attempt.trace_id) AS trace_attr,
		        event_source, gram_urn, toString(ifNull(trace_id, '')) AS trace_id
		 FROM telemetry_logs
		 WHERE gram_project_id = ? AND event_urn = ?
		 ORDER BY stage = 'discovery_listing' DESC, mcp_server_id`,
		projectID.String(), MCPConnectionAttemptEventURN))
	require.Len(t, rows, 4)
	require.Equal(t, []string{attemptStageListing, string(outcomePartial), "", "", subject.String(), "credential:user"}, []string{rows[0].Stage, rows[0].Outcome, rows[0].Reason, rows[0].MemberID, rows[0].Subject, rows[0].ClientKey})
	for _, r := range rows {
		require.Empty(t, r.TraceID, "the request's trace stays out of the trace column")
		require.Equal(t, caller.traceID, r.TraceAttr)
		if r.MemberID == linear.serverID.String() {
			require.Equal(t, []string{string(outcomeFailed), "rate_limited", provider.String(), "2", "meta_discovery", "metamcp:" + gatewayID.String() + ":tools/list"}, []string{r.Outcome, r.Reason, r.Provider, r.Retries, r.Source, r.ToolURN})
			continue
		}
		require.Empty(t, r.Source)
		require.Empty(t, r.ToolURN)
	}

	var listingAttempts, memberAttempts, failed, recovered, retries uint64
	require.NoError(t, chConn.QueryRow(ctx,
		`SELECT sumIf(attempts, stage = 'discovery_listing'), sumIf(attempts, stage = 'discovery_member'),
		        sumIf(attempts, stage = 'discovery_member' AND outcome = 'failed'), sum(recovered), sum(retries)
		 FROM mcp_connection_attempt_summaries
		 WHERE gram_project_id = toUUID(?) AND server_kind = 'meta' AND server_id = ?`,
		projectID.String(), gatewayID.String()).Scan(&listingAttempts, &memberAttempts, &failed, &recovered, &retries))
	require.Equal(t, []uint64{1, 3, 1, 1, 3}, []uint64{listingAttempts, memberAttempts, failed, recovered, retries})

	logs, err := telemetryrepo.New(chConn).ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
		GramProjectID: projectID.String(),
		TimeStart:     observedAt.Add(-time.Minute).UnixNano(),
		TimeEnd:       observedAt.Add(time.Minute).UnixNano(),
		GramURNs:      []string{"metamcp:" + gatewayID.String() + ":tools/list"},
		EventSource:   string(tm.EventSourceMetaDiscovery),
		SortOrder:     "desc",
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	var omission struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		HTTP struct {
			Response struct {
				StatusCode int `json:"status_code"`
			} `json:"response"`
		} `json:"http"`
	}
	require.NoError(t, json.Unmarshal([]byte(logs[0].Attributes), &omission))
	require.Equal(t, `server "linear" was rate limited and was left out of the tool listing`, omission.Error.Message)
	require.Equal(t, http.StatusTooManyRequests, omission.HTTP.Response.StatusCode)

	var traces, toolCalls, chatRequests uint64
	require.NoError(t, chConn.QueryRow(ctx, `SELECT count() FROM trace_summaries WHERE gram_project_id = ?`, projectID.String()).Scan(&traces))
	require.NoError(t, chConn.QueryRow(ctx,
		`SELECT countIfMerge(total_tool_calls), countIfMerge(total_chat_requests) FROM metrics_summaries WHERE gram_project_id = ?`,
		projectID.String()).Scan(&toolCalls, &chatRequests))
	require.Equal(t, []uint64{0, 0, 0}, []uint64{traces, toolCalls, chatRequests})
}

// heldInsert holds each telemetry insert until the test releases it.
type heldInsert struct {
	clickhouse.Conn
	started chan context.Context
	release chan struct{}
}

func (h heldInsert) Exec(ctx context.Context, _ string, _ ...any) error {
	h.started <- ctx
	<-h.release
	return nil
}

// insertsDone hears each batch the logger committed.
type insertsDone chan struct{}

func (d insertsDone) OnTelemetryLogsWritten(context.Context, []tm.LogParams) { d <- struct{}{} }

// Recording returns while the insert is still held, and the write outlives the
// request that made it, within its own bound.
func TestRecordDirectListingWritesAfterTheRequestWithinItsBound(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	conn := heldInsert{Conn: nil, started: make(chan context.Context, 1), release: make(chan struct{})}
	telemLogger := tm.NewLogger(context.Background(), logger, nil, nil, conn, enabledForAnyOrganization, enabledForAnyOrganization, nil, tm.NewNoopLogPublisher(logger))
	done := make(insertsDone, 1)
	telemLogger.AddObserver(done)
	s := &Service{logger: logger, telemLogger: telemLogger}
	request, cancel := context.WithCancel(t.Context())
	cancel()
	gate := &metaGateContext{projectID: uuid.New(), organizationID: "org_" + uuid.NewString(), metaServerID: uuid.New()}

	begun := time.Now()
	s.recordDirectListing(request, gate, begun, listingFailure(request, classInternal, reasonInventoryError), nil)
	write := <-conn.started
	require.NoError(t, write.Err(), "the request's cancellation does not cancel the write")
	deadline, ok := write.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, begun.Add(connectionAttemptWriteTimeout), deadline, time.Second)
	close(conn.release)
	<-done
}
