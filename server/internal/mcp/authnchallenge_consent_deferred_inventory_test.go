package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	inventoryDeferredAttr    = `data-inventory-deferred="true"`
	inventoryNotDeferredAttr = `data-inventory-deferred="false"`
)

func renderToolsIsland(t *testing.T, fx consentActionFixture) string {
	t.Helper()
	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, `id="consent-tools-root"`)
	return page
}

func TestConsentPage_ChainedServiceDefersToolInventory(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim524-deferred-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, inventoryDeferredAttr)
	require.Contains(t, page, `data-consent-enabled="true"`)

	// The transport still answers "not connected"; the page flag is what
	// turns that answer into the deferred state.
	requireConsentInventoryConflict(t, fx, uuid.Nil)
}

// requireConsentInventoryConflict asks the consent transport for tools as
// authClient, or as the page's preselected credential when it is nil, and
// requires the "not connected" answer.
func requireConsentInventoryConflict(t *testing.T, fx consentActionFixture, authClient uuid.UUID) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Consent-State", fx.stateID)
	req.Header.Set("Gram-Consent-Csrf", "csrf-token")
	req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
	if authClient != uuid.Nil {
		req.Header.Set("Gram-Consent-Auth-Client", authClient.String())
	}
	err := fx.ti.service.ServeConsentMCP(httptest.NewRecorder(), req, fx.endpoint)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeConflict, se.Code)
}

// logRecords returns every JSON log record in logs whose message is msg.
func logRecords(t *testing.T, logs *syncBuffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// seedLoggedChainedConsent is seedChainedConsentEndpoint over a service
// whose logs are captured.
func seedLoggedChainedConsent(t *testing.T, tag string) (context.Context, consentActionFixture, uuid.UUID, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{mu: sync.Mutex{}, buf: bytes.Buffer{}}
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil})), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil)
	projectID, orgID := consentTestTenant(t, ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := tag + "-" + uuid.NewString()[:8]
	serverID := attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, issuerID, slug)
	endpoint.McpServerID = conv.ToNullUUID(serverID)
	endpoint.UpstreamResource = consentChainedUpstream
	fx := consentActionFixture{ti: ti, endpoint: endpoint, stateID: stateID, projectID: projectID, orgID: orgID, shared: issuerID, subject: subject, clientA: uuid.Nil, clientB: uuid.Nil, clientC: uuid.Nil, clientD: uuid.Nil}
	registerPageClient(t, ctx, fx)
	fx.clientA = createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, tag, "", []uuid.UUID{issuerID})
	return ctx, fx, clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, fx.clientA), logs
}

const (
	deferredConflictLog = "consent tool inventory waits for identity chaining"
	realConflictLog     = "connect the upstream service before choosing tools"
)

func TestConsentInventory_DeferredConflictLogsAtInfo(t *testing.T) {
	t.Parallel()

	_, fx, issuer, logs := seedLoggedChainedConsent(t, "aim524-log-deferred")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	requireConsentInventoryConflict(t, fx, uuid.Nil)
	deferred := logRecords(t, logs, deferredConflictLog)
	require.Len(t, deferred, 1)
	require.Equal(t, "INFO", deferred[0]["level"])
	require.Equal(t, true, deferred[0]["gram.consent.inventory_deferred"])
	require.Empty(t, logRecords(t, logs, realConflictLog))
}

func TestConsentInventory_StoredSelectionConflictStaysWarn(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer, logs := seedLoggedChainedConsent(t, "aim524-log-prefill")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	seedSubjectToolSelection(t, ctx, fx, []string{"get_thing"})

	requireConsentInventoryConflict(t, fx, uuid.Nil)
	require.Empty(t, logRecords(t, logs, deferredConflictLog), "the page keeps blocking, so the conflict is not expected")
	warned := logRecords(t, logs, realConflictLog)
	require.Len(t, warned, 1)
	require.Equal(t, "WARN", warned[0]["level"])
	require.NotContains(t, warned[0], "gram.consent.inventory_deferred")
}

func TestConsentInventory_AmbiguousBindingConflictStaysWarn(t *testing.T) {
	t.Parallel()

	_, fx, issuer, logs := seedLoggedChainedConsent(t, "aim524-log-ambiguous")
	g := setConsentGovernor(t, fx, issuer)
	// Governed but served by no single binding: the page renders no chained card.
	g.governs = true

	requireConsentInventoryConflict(t, fx, uuid.Nil)
	require.Empty(t, logRecords(t, logs, deferredConflictLog))
	warned := logRecords(t, logs, realConflictLog)
	require.Len(t, warned, 1)
	require.Equal(t, "WARN", warned[0]["level"])
}

func TestConsentPage_LinkedServiceDoesNotDeferToolInventory(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-linked-page")
	setConsentGovernor(t, fx, issuer)
	grant(t, ctx, fx, fx.clientA, consentChainedUpstream)

	require.Contains(t, renderToolsIsland(t, fx), inventoryNotDeferredAttr)
}

func TestConsentPage_UnconnectedUnchainedServicesDoNotDefer(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-unchained-page")
	setConsentGovernor(t, fx, issuer)
	// A second unlinked card keeps the page from auto-connecting the first.
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim524-unchained-page-b", "", []uuid.UUID{fx.shared})

	page := renderToolsIsland(t, fx)
	require.NotContains(t, page, consentManagedCopy)
	require.Contains(t, page, inventoryNotDeferredAttr, "an unlinked, unchained service's 409 keeps blocking approval")
}

// Without a credential choice the inventory signs in through every service,
// so the connected card's token serves it.
func TestConsentPage_ConnectedCardBesideChainedCardDoesNotDeferWithoutCredentialChoice(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-mixed-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim524-mixed-page-b", "", []uuid.UUID{fx.shared})
	grant(t, ctx, fx, fx.clientB, consentChainedUpstream)

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, consentManagedCopy, "card A stays chained")
	require.Contains(t, page, inventoryNotDeferredAttr, "a connected card's 409 must not read as deferred")
}

// chainedCredentialChoices are the credentials a person chooses between on
// seedChainedCredentialChoice's page.
type chainedCredentialChoices struct {
	// identityProvider signs in through the chained registration.
	identityProvider uuid.UUID

	// chainedPersonal is a personal client on the chained registration.
	chainedPersonal uuid.UUID

	// separatePersonal is a personal client on a registration identity
	// chaining does not serve.
	separatePersonal uuid.UUID
}

// seedChainedCredentialChoice is seedLoggedChainedConsent for an organization
// whose people choose a typed auth client. Its page shows two services: the
// chained registration, and a second one stored in the fixture's clientB.
func seedChainedCredentialChoice(t *testing.T, tag string) (context.Context, consentActionFixture, chainedCredentialChoices, *syncBuffer) {
	t.Helper()
	ctx, fx, issuer, logs := seedLoggedChainedConsent(t, tag)
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	fx.ti.service.manual = true
	fx.ti.service.SetAuthClientPermitted(true)
	_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	fx.clientB = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag+"-separate", "", []uuid.UUID{fx.shared})

	fixtures := authclientfixtures.New(fx.ti.conn)
	offer := func(clientType string, registration uuid.UUID) uuid.UUID {
		id, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: conv.ToNullUUID(fx.projectID), ClientType: clientType, Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
		require.NoError(t, err)
		require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: fx.endpoint.McpServerID.UUID, AuthClientID: id, Source: "admin"}))
		return id
	}
	return ctx, fx, chainedCredentialChoices{
		identityProvider: offer("ema", fx.clientA),
		chainedPersonal:  offer("remote_session", fx.clientA),
		separatePersonal: offer("remote_session", fx.clientB),
	}, logs
}

// requirePageInventoryDeferral checks the tool island's deferral for the
// credential the page preselects.
func requirePageInventoryDeferral(t *testing.T, page string, deferred bool) {
	t.Helper()
	if deferred {
		require.Regexp(t, `id="consent-tools-root"[^>]*`+inventoryDeferredAttr, page)
		return
	}
	require.Regexp(t, `id="consent-tools-root"[^>]*`+inventoryNotDeferredAttr, page)
}

// requireCredentialInventoryDeferral checks that choosing authClient on the
// page defers the tool inventory until service's identity chaining runs, or,
// with a nil service, that it does not defer.
func requireCredentialInventoryDeferral(t *testing.T, page string, authClient uuid.UUID, service uuid.UUID) {
	t.Helper()
	option := `id="auth-client-` + authClient.String() + `"`
	require.Contains(t, page, option)
	if service == uuid.Nil {
		require.NotRegexp(t, option+`[^>]*data-deferred-service`, page)
		return
	}
	require.Regexp(t, option+`[^>]*data-deferred-service="`+service.String()+`"`, page)
}

func TestConsentPage_ChainedCredentialDefersBesideUnconnectedService(t *testing.T) {
	t.Parallel()

	_, fx, choices, _ := seedChainedCredentialChoice(t, "aim896-mixed-page")

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, "0 of 2 connected")
	require.Contains(t, page, `data-consent-enabled="true"`)
	requirePageInventoryDeferral(t, page, true)
	requireCredentialInventoryDeferral(t, page, choices.identityProvider, fx.clientA)
	requireCredentialInventoryDeferral(t, page, choices.chainedPersonal, fx.clientA)
	requireCredentialInventoryDeferral(t, page, choices.separatePersonal, uuid.Nil)
}

func TestConsentPage_ChainedCredentialStillDefersOnceAnotherServiceConnects(t *testing.T) {
	t.Parallel()

	ctx, fx, choices, _ := seedChainedCredentialChoice(t, "aim896-connected-page")
	grant(t, ctx, fx, fx.clientB, consentChainedUpstream)

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, `data-consent-enabled="true"`)
	requirePageInventoryDeferral(t, page, true)
	requireCredentialInventoryDeferral(t, page, choices.identityProvider, fx.clientA)
	requireCredentialInventoryDeferral(t, page, choices.separatePersonal, uuid.Nil)
}

func TestConsentPage_StoredSelectionDoesNotDeferChainedCredential(t *testing.T) {
	t.Parallel()

	ctx, fx, choices, logs := seedChainedCredentialChoice(t, "aim896-prefill-page")
	seedSubjectToolSelection(t, ctx, fx, []string{"get_thing"})

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, `data-prefill=`)
	requirePageInventoryDeferral(t, page, false)
	requireCredentialInventoryDeferral(t, page, choices.identityProvider, uuid.Nil)
	requireCredentialInventoryDeferral(t, page, choices.chainedPersonal, uuid.Nil)

	requireConsentInventoryConflict(t, fx, choices.identityProvider)
	require.Empty(t, logRecords(t, logs, deferredConflictLog), "the page keeps blocking, so the conflict is not expected")
	require.Len(t, logRecords(t, logs, realConflictLog), 1)
}

func TestConsentInventory_ChainedCredentialConflictLogsAtInfoBesideUnconnectedService(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		choose func(chainedCredentialChoices) uuid.UUID
	}{
		{name: "preselected identity provider", choose: func(chainedCredentialChoices) uuid.UUID { return uuid.Nil }},
		{name: "identity provider", choose: func(c chainedCredentialChoices) uuid.UUID { return c.identityProvider }},
		{name: "personal client on the chained registration", choose: func(c chainedCredentialChoices) uuid.UUID { return c.chainedPersonal }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, fx, choices, logs := seedChainedCredentialChoice(t, "aim896-log-deferred")

			requireConsentInventoryConflict(t, fx, tc.choose(choices))
			deferred := logRecords(t, logs, deferredConflictLog)
			require.Len(t, deferred, 1)
			require.Equal(t, "INFO", deferred[0]["level"])
			require.Empty(t, logRecords(t, logs, realConflictLog))
		})
	}
}

func TestConsentPage_StoredSelectionDoesNotDeferToolInventory(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-prefill-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	seedSubjectToolSelection(t, ctx, fx, []string{"get_thing"})

	page := renderToolsIsland(t, fx)
	require.Contains(t, page, `data-prefill=`)
	require.Contains(t, page, inventoryNotDeferredAttr, "deferring would replace the stored selection with all tools")
}

// TestConsentPost_DeferredInventoryApprovesAllTools pins existing behavior
// the deferred state relies on: filtering off with no inventory approves the
// unrestricted grant.
func TestConsentPost_DeferredInventoryApprovesAllTools(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim524-deferred-approve")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "approve")
	form.Set("tool_filtering", "off")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	stored, err := grantCache.Get(ctx, "userSessionGrant:"+fx.shared.String()+":"+code)
	require.NoError(t, err)
	require.Nil(t, stored.ToolSelection, "no selection is the unrestricted all-tools grant")
}

// seedSubjectToolSelection gives the fixture subject a live session on the
// page's client holding a restrictive selection for this endpoint.
func seedSubjectToolSelection(t *testing.T, ctx context.Context, fx consentActionFixture, tools []string) {
	t.Helper()
	entries := make([]toolfilter.AllowEntry, 0, len(tools))
	for _, name := range tools {
		entries = append(entries, toolfilter.AllowEntry{Type: toolfilter.AllowTypeTool, Name: name, Mode: nil, Tools: nil})
	}
	sel, err := toolfilter.NewSessionSelection("mcp_server:"+fx.endpoint.McpServerID.UUID.String(), uuid.New(), entries)
	require.NoError(t, err)
	raw, err := json.Marshal(sel)
	require.NoError(t, err)
	seedSubjectSessionPolicy(t, ctx, fx, raw)
}

// seedSubjectSessionPolicy stores raw as the tool policy of a live session
// for the fixture subject and page client.
func seedSubjectSessionPolicy(t *testing.T, ctx context.Context, fx consentActionFixture, raw []byte) {
	t.Helper()
	q := usersessions_repo.New(fx.ti.conn)
	client, err := q.GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: fx.shared,
		ClientID:            "age3328-mcp-client",
	})
	require.NoError(t, err)

	sum := sha256.Sum256([]byte("refresh-" + uuid.NewString()))
	_, err = q.CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: fx.shared,
		UserSessionClientID: conv.ToNullUUID(client.ID),
		SubjectUrn:          fx.subject,
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    conv.ToPGText(base64.RawURLEncoding.EncodeToString(sum[:])),
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
		ToolSelection:       raw,
		SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
}
