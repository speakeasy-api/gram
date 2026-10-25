package platformmcp

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/require"

	mcpeventsv1 "github.com/speakeasy-api/gram/infra/gen/gram/mcpevents/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpevents"
	eventrepo "github.com/speakeasy-api/gram/server/internal/mcpevents/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type receivedReviewEvent struct {
	body    []byte
	headers http.Header
}

type eventFixture struct {
	db         *pgxpool.Pool
	service    *mcpevents.Service
	authorizer *EventAuthorizer
	flags      *feature.InMemory
	owner      mcpevents.Owner
	principal  Principal
	input      mcpevents.SubscriptionInput
	requestID  uuid.UUID
	received   chan receivedReviewEvent
	status     *atomic.Int32
}

func newEventFixture(t *testing.T) eventFixture {
	t.Helper()
	ctx := t.Context()
	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_events")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, db)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, db, principal.OrganizationID))
	seedPlatformMCPAuthorizationMember(t, ctx, db, principal.OrganizationID, principal.UserID, authz.SystemRoleAdmin)
	connection, err := platformrepo.New(db).GetActivePlatformMCPConnectionByID(ctx, platformrepo.GetActivePlatformMCPConnectionByIDParams{ID: uuid.MustParse(principal.ConnectionID), OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	principal.ClientID = connection.ClientID
	owner, err := eventOwner(principal)
	require.NoError(t, err)
	_, err = productrepo.New(db).EnableFeature(ctx, productrepo.EnableFeatureParams{OrganizationID: owner.OrganizationID, FeatureName: string(productfeatures.FeaturePlatformMCP)})
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagMCPApproval, owner.OrganizationID, true)
	flags.SetFlag(feature.FlagPlatformMCPEvents, owner.OrganizationID, true)
	request, err := approvalrepo.New(db).UpsertApprovalRequest(ctx, approvalrepo.UpsertApprovalRequestParams{
		OrganizationID: owner.OrganizationID, ProjectID: project.ID, TargetKind: "server_url", TargetRaw: "https://events.example.test/mcp", TargetKey: "https://events.example.test/mcp", Status: "requested",
	})
	require.NoError(t, err)
	_, err = approvalrepo.New(db).UpsertApprovalRequestRequester(ctx, approvalrepo.UpsertApprovalRequestRequesterParams{OrganizationID: owner.OrganizationID, ProjectID: project.ID, McpApprovalRequestID: request.ID, UserID: owner.UserID})
	require.NoError(t, err)
	// One pooled connection turns any nested acquisition into a test timeout.
	config := db.Config()
	config.MaxConns = 1
	limited, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(limited.Close)
	authorizer := NewEventAuthorizer(limited, flags)
	received := make(chan receivedReviewEvent, 32)
	status := &atomic.Int32{}
	status.Store(http.StatusOK)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var control struct {
			Challenge string `json:"challenge"`
		}
		if json.Unmarshal(body, &control) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if control.Challenge != "" {
			// A receiver may need the server's sole pool connection while verifying.
			// This fails if Subscribe holds a transaction during the callback.
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if err := limited.Ping(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(control)
			return
		}
		received <- receivedReviewEvent{body: body, headers: r.Header.Clone()}
		w.WriteHeader(int(status.Load()))
	}))
	t.Cleanup(server.Close)
	certs := x509.NewCertPool()
	certs.AddCert(server.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(certs))
	require.NoError(t, err)
	encryptionClient, err := encryption.NewWithBytes([]byte(strings.Repeat("e", 32)))
	require.NoError(t, err)
	service, err := mcpevents.NewService(testenv.NewLogger(t), limited, encryptionClient, authorizer, mcpevents.NewWebhook(policy))
	require.NoError(t, err)
	input := mcpevents.SubscriptionInput{Name: mcpevents.ReviewRequested, Arguments: mcpevents.Arguments{ProjectID: project.ID}, Delivery: mcpevents.Destination{Mode: "webhook", URL: server.URL, Secret: "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))}}
	return eventFixture{db: limited, service: service, authorizer: authorizer, flags: flags, owner: owner, principal: principal, input: input, requestID: request.ID, received: received, status: status}
}

func routeReviewEvent(t *testing.T, ctx context.Context, f eventFixture, eventName string, recipients []string) (*mcpeventsv1.ReviewChanged, []eventrepo.PlatformMcpEventDelivery) {
	t.Helper()
	event := mcpeventsv1.ReviewChanged_builder{EventId: new(uuid.NewString()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String()), RequestId: new(f.requestID.String()), EventName: new(eventName), OccurredAt: new(time.Now().UTC().Format(time.RFC3339Nano)), RecipientUserIds: recipients}.Build()
	require.NoError(t, f.service.Route(ctx, event, gcp.MessageMetadata{}))
	require.NoError(t, f.service.Route(ctx, event, gcp.MessageMetadata{}))
	deliveries, err := eventrepo.New(f.db).ListEventDeliveries(ctx, eventrepo.ListEventDeliveriesParams{OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID, EventID: uuid.MustParse(event.GetEventId())})
	require.NoError(t, err)
	return event, deliveries
}

func TestEventsRetryWithSingleDatabaseConnection(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	event, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 1)
	message := mcpeventsv1.Delivery_builder{Id: new(deliveries[0].ID.String()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String())}.Build()
	f.status.Store(http.StatusTooEarly)
	require.Error(t, f.service.Deliver(ctx, message, gcp.MessageMetadata{}))
	first := <-f.received
	f.status.Store(http.StatusOK)
	require.NoError(t, f.service.Deliver(ctx, message, gcp.MessageMetadata{}))
	second := <-f.received
	require.Equal(t, first.body, second.body)
	require.Equal(t, event.GetEventId(), first.headers.Get("webhook-id"))
	signer, err := standardwebhooks.NewWebhook(f.input.Delivery.Secret)
	require.NoError(t, err)
	require.NoError(t, signer.Verify(second.body, second.headers))
	require.NoError(t, f.service.Deliver(ctx, message, gcp.MessageMetadata{}))
	require.Empty(t, f.received)
	rows, err := eventrepo.New(f.db).ListEventDeliveries(ctx, eventrepo.ListEventDeliveriesParams{OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID, EventID: uuid.MustParse(event.GetEventId())})
	require.NoError(t, err)
	require.Equal(t, "delivered", rows[0].Status)
	require.EqualValues(t, 2, rows[0].Attempts)
}

func TestEventsStopAfterAuthorizationChanges(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"flag", "demotion", "connection"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := newEventFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			_, err := f.service.Subscribe(ctx, f.owner, f.input)
			require.NoError(t, err)
			_, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
			require.Len(t, deliveries, 1)
			switch change {
			case "flag":
				f.flags.SetFlag(feature.FlagPlatformMCPEvents, f.owner.OrganizationID, false)
			case "demotion":
				_, err = accessrepo.New(f.db).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: f.owner.OrganizationID, WorkosUserID: f.owner.UserID})
				require.NoError(t, err)
				seedPlatformMCPAuthorizationMember(t, ctx, f.db, f.owner.OrganizationID, f.owner.UserID, authz.SystemRoleMember)
			case "connection":
				_, err = platformrepo.New(f.db).RevokePlatformMCPConnection(ctx, platformrepo.RevokePlatformMCPConnectionParams{ID: f.owner.ConnectionID, OrganizationID: f.owner.OrganizationID, RevokedAt: conv.ToPGTimestamptz(time.Now())})
				require.NoError(t, err)
			}
			message := mcpeventsv1.Delivery_builder{Id: new(deliveries[0].ID.String()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String())}.Build()
			require.NoError(t, f.service.Deliver(ctx, message, gcp.MessageMetadata{}))
			require.Empty(t, f.received)
			_, err = f.service.Subscribe(ctx, f.owner, f.input)
			require.ErrorIs(t, err, mcpevents.ErrDenied)
		})
	}
}

func TestEventsUnsubscribeAndRenewDoNotResurrectOldDeliveries(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	first, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	_, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 1)
	require.NoError(t, f.service.Unsubscribe(ctx, f.owner, f.input))
	second, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	subscription, err := eventrepo.New(f.db).GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: second.ID, OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID})
	require.NoError(t, err)
	require.NotEqual(t, deliveries[0].SubscriptionGeneration, subscription.Generation)
	message := mcpeventsv1.Delivery_builder{Id: new(deliveries[0].ID.String()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String())}.Build()
	require.NoError(t, f.service.Deliver(ctx, message, gcp.MessageMetadata{}))
	require.Empty(t, f.received)
}

func TestEventsRotationSignsWithBothKeys(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	first, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	rotated := f.input
	rotated.Delivery.Secret = "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("r", 32)))
	second, err := f.service.Subscribe(ctx, f.owner, rotated)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	_, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 1)
	message := mcpeventsv1.Delivery_builder{Id: new(deliveries[0].ID.String()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String())}.Build()
	require.NoError(t, f.service.Deliver(ctx, message, gcp.MessageMetadata{}))
	received := <-f.received
	for _, secret := range []string{f.input.Delivery.Secret, rotated.Delivery.Secret} {
		signer, err := standardwebhooks.NewWebhook(secret)
		require.NoError(t, err)
		require.NoError(t, signer.Verify(received.body, received.headers))
	}
	row, err := eventrepo.New(f.db).GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: first.ID, OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID})
	require.NoError(t, err)
	require.NotContains(t, row.SigningSecret, rotated.Delivery.Secret)
	require.NotContains(t, row.PreviousSigningSecret.String, f.input.Delivery.Secret)
}

func TestEventsOnlyRouteToDecisionRecipients(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	f.input.Name = mcpevents.ReviewDecided
	_, err := f.service.Subscribe(t.Context(), f.owner, f.input)
	require.NoError(t, err)
	_, deliveries := routeReviewEvent(t, t.Context(), f, mcpevents.ReviewDecided, []string{"another-user"})
	require.Empty(t, deliveries)
	_, deliveries = routeReviewEvent(t, t.Context(), f, mcpevents.ReviewDecided, []string{f.owner.UserID})
	require.Len(t, deliveries, 1)
	foreign := f.input
	foreign.Arguments.ProjectID = uuid.New()
	_, err = f.service.Subscribe(t.Context(), f.owner, foreign)
	require.ErrorIs(t, err, mcpevents.ErrDenied)
}

func TestEventsRetentionPreservesDeduplicationHorizon(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	subscription, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	event, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 1)
	now := time.Now()
	require.NoError(t, mcpevents.Cleanup(ctx, f.db, now.Add(25*time.Hour)))
	q := eventrepo.New(f.db)
	_, err = q.GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: subscription.ID, OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID})
	require.NoError(t, err)
	// A parent with retained history must not cascade an unbounded FK update.
	deleted, err := q.CleanupExpiredEventSubscriptions(ctx, eventrepo.CleanupExpiredEventSubscriptionsParams{Cutoff: conv.ToPGTimestamptz(now.Add(25 * time.Hour)), BatchSize: 1})
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.NoError(t, mcpevents.Cleanup(ctx, f.db, now.Add(49*time.Hour)))
	_, err = q.GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: subscription.ID, OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	params := eventrepo.ListEventDeliveriesParams{OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID, EventID: uuid.MustParse(event.GetEventId())}
	rows, err := q.ListEventDeliveries(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].SubscriptionID.Valid)
	require.NoError(t, mcpevents.Cleanup(ctx, f.db, now.Add(8*24*time.Hour)))
	rows, err = q.ListEventDeliveries(ctx, params)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func eventRPC(t *testing.T, handler http.Handler, method string, input *mcpevents.SubscriptionInput) (json.RawMessage, *jsonrpc.Error) {
	t.Helper()
	params := struct {
		mcp.ParamsBase
		*mcpevents.SubscriptionInput
	}{ParamsBase: mcp.ParamsBase{Meta: mcp.Meta{mcp.MetaKeyProtocolVersion: "2026-07-28", mcp.MetaKeyClientCapabilities: mcp.ClientCapabilities{}, mcp.MetaKeyClientInfo: mcp.Implementation{Name: "event-test", Version: "1"}}}, SubscriptionInput: input}
	body, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *jsonrpc.Error  `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	return response.Result, response.Error
}

func TestEventsHTTPDiscoveryAndSubscription(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	runtime := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: f.principal}, testGate{enabled: true}, &testAuthorizer{}, "", "test-cursor-key", nil, nil, nil, nil, nil)
	require.NoError(t, runtime.RegisterReviewEvents(f.service, f.authorizer, allowBudget()))
	handler := runtime.Handler()
	raw, rpcErr := eventRPC(t, handler, "server/discover", nil)
	require.Nil(t, rpcErr, string(raw))
	var discovery struct {
		CacheScope   string `json:"cacheScope"`
		TTLMs        int64  `json:"ttlMs"`
		Capabilities struct {
			Events *struct{} `json:"events"`
		} `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	require.NotNil(t, discovery.Capabilities.Events, string(raw))
	require.Equal(t, "private", discovery.CacheScope)
	require.Zero(t, discovery.TTLMs)
	raw, rpcErr = eventRPC(t, handler, "events/list", nil)
	require.Nil(t, rpcErr)
	var list eventListResult
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list.Events, 2)
	raw, rpcErr = eventRPC(t, handler, "events/subscribe", &f.input)
	require.Nil(t, rpcErr)
	var subscription mcpevents.SubscriptionResult
	require.NoError(t, json.Unmarshal(raw, &subscription))
	require.NotEmpty(t, subscription.ID)
	unknown := f.input
	unknown.Name = "review.unknown"
	_, rpcErr = eventRPC(t, handler, "events/subscribe", &unknown)
	require.NotNil(t, rpcErr)
	require.EqualValues(t, eventNotFound, rpcErr.Code)
	_, rpcErr = eventRPC(t, handler, "events/unsubscribe", &f.input)
	require.Nil(t, rpcErr)
	f.flags.SetFlag(feature.FlagPlatformMCPEvents, f.owner.OrganizationID, false)
	raw, rpcErr = eventRPC(t, handler, "events/list", nil)
	require.Nil(t, rpcErr)
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Empty(t, list.Events)
	_, rpcErr = eventRPC(t, handler, "events/subscribe", &f.input)
	require.NotNil(t, rpcErr)
	require.EqualValues(t, eventForbidden, rpcErr.Code)
}

func TestEventsExpiredRefreshStartsNewLifetime(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	f.input.TTLMs = new(int64(1))
	first, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Minute), first.RefreshBefore, time.Second)
	q := eventrepo.New(f.db)
	old, err := q.GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: first.ID, ProjectID: f.input.Arguments.ProjectID, OrganizationID: f.owner.OrganizationID})
	require.NoError(t, err)
	_, err = q.SaveSubscription(ctx, eventrepo.SaveSubscriptionParams{
		ID: old.ID, ProjectID: old.ProjectID, OrganizationID: old.OrganizationID, ConnectionID: old.ConnectionID, ConnectionGeneration: old.ConnectionGeneration,
		ClientID: old.ClientID, UserID: old.UserID, EventName: old.EventName, CallbackUrl: old.CallbackUrl, SigningSecret: old.SigningSecret,
		PreviousSigningSecret: old.PreviousSigningSecret, PreviousSecretExpiresAt: old.PreviousSecretExpiresAt, Generation: old.Generation,
		SubscribedAt: old.SubscribedAt, VerifiedAt: old.VerifiedAt, ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Second)),
	})
	require.NoError(t, err)
	f.input.TTLMs = new(int64(math.MaxInt64))
	second, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.WithinDuration(t, time.Now().Add(mcpevents.DefaultTTL), second.RefreshBefore, time.Second)
	renewed, err := q.GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: second.ID, ProjectID: f.input.Arguments.ProjectID, OrganizationID: f.owner.OrganizationID})
	require.NoError(t, err)
	require.NotEqual(t, old.Generation, renewed.Generation)
}

func TestEventsQuotaBoundsMultipleSubscriptions(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	for i := range 20 {
		input := f.input
		input.Delivery.URL += fmt.Sprintf("/chat/%d", i)
		_, err := f.service.Subscribe(ctx, f.owner, input)
		require.NoError(t, err)
	}
	_, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.ErrorIs(t, err, mcpevents.ErrLimit)
	_, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 20)
}

// failingEventAuthorizer injects a genuine statement error to prove the attempt
// counter survives PostgreSQL's aborted-transaction state via the savepoint.
type failingEventAuthorizer struct{ tx pgx.Tx }

func (a failingEventAuthorizer) WithTx(tx pgx.Tx) mcpevents.Authorizer {
	return failingEventAuthorizer{tx: tx}
}
func (a failingEventAuthorizer) Authorize(ctx context.Context, owner mcpevents.Owner, projectID uuid.UUID, eventName string, requestID uuid.UUID) error {
	_, err := eventrepo.New(a.tx).CreateDelivery(ctx, eventrepo.CreateDeliveryParams{
		ID: uuid.New(), ProjectID: projectID, OrganizationID: owner.OrganizationID,
		SubscriptionID: conv.ToPGText("missing-subscription"), SubscriptionGeneration: uuid.New(),
		EventID: uuid.New(), EventName: eventName, RequestID: requestID, Payload: []byte(`{}`),
	})
	if err == nil {
		return errors.New("expected missing subscription")
	}
	return fmt.Errorf("inject failed authorization statement: %w", err)
}

func TestEventsAuthorizationSQLFailuresExhaustAttemptBudget(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	_, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	event, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 1)
	key, err := encryption.NewWithBytes([]byte(strings.Repeat("e", 32)))
	require.NoError(t, err)
	service, err := mcpevents.NewService(testenv.NewLogger(t), f.db, key, failingEventAuthorizer{tx: nil}, mcpevents.NewWebhook(guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))))
	require.NoError(t, err)
	message := mcpeventsv1.Delivery_builder{Id: new(deliveries[0].ID.String()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String())}.Build()
	for range 7 {
		require.Error(t, service.Deliver(ctx, message, gcp.MessageMetadata{}))
	}
	require.NoError(t, service.Deliver(ctx, message, gcp.MessageMetadata{}))
	require.NoError(t, service.Deliver(ctx, message, gcp.MessageMetadata{}))
	rows, err := eventrepo.New(f.db).ListEventDeliveries(ctx, eventrepo.ListEventDeliveriesParams{OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID, EventID: uuid.MustParse(event.GetEventId())})
	require.NoError(t, err)
	require.EqualValues(t, 8, rows[0].Attempts)
	require.Equal(t, "failed", rows[0].Status)
	require.Empty(t, f.received)
}

func TestEventsDecryptionFailureIsTerminal(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	_, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	event, deliveries := routeReviewEvent(t, ctx, f, mcpevents.ReviewRequested, nil)
	require.Len(t, deliveries, 1)
	wrongKey, err := encryption.NewWithBytes([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	service, err := mcpevents.NewService(testenv.NewLogger(t), f.db, wrongKey, f.authorizer, mcpevents.NewWebhook(guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))))
	require.NoError(t, err)
	message := mcpeventsv1.Delivery_builder{Id: new(deliveries[0].ID.String()), OrganizationId: new(f.owner.OrganizationID), ProjectId: new(f.input.Arguments.ProjectID.String())}.Build()
	require.NoError(t, service.Deliver(ctx, message, gcp.MessageMetadata{}))
	require.NoError(t, service.Deliver(ctx, message, gcp.MessageMetadata{}))
	rows, err := eventrepo.New(f.db).ListEventDeliveries(ctx, eventrepo.ListEventDeliveriesParams{OrganizationID: f.owner.OrganizationID, ProjectID: f.input.Arguments.ProjectID, EventID: uuid.MustParse(event.GetEventId())})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows[0].Attempts)
	require.Equal(t, "failed", rows[0].Status)
	require.Empty(t, f.received)
}

func TestEventsStorageAndReviewReadsRejectCrossOrganizationReferences(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	ctx := t.Context()
	other, otherProject := seedRegistrationLifecycle(t, ctx, f.db)
	subscription, err := f.service.Subscribe(ctx, f.owner, f.input)
	require.NoError(t, err)
	q := eventrepo.New(f.db)
	_, err = q.GetReviewEventRequest(ctx, eventrepo.GetReviewEventRequestParams{ID: f.requestID, ProjectID: f.input.Arguments.ProjectID, OrganizationID: other.OrganizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = approvalrepo.New(f.db).GetApprovalRequestByTarget(ctx, approvalrepo.GetApprovalRequestByTargetParams{ProjectID: f.input.Arguments.ProjectID, OrganizationID: other.OrganizationID, TargetKind: "server_url", TargetKey: "https://events.example.test/mcp"})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	row, err := q.GetSubscription(ctx, eventrepo.GetSubscriptionParams{ID: subscription.ID, ProjectID: f.input.Arguments.ProjectID, OrganizationID: f.owner.OrganizationID})
	require.NoError(t, err)
	params := eventrepo.SaveSubscriptionParams{
		ID: "cross-tenant-project", ProjectID: otherProject.ID, OrganizationID: row.OrganizationID,
		ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration, ClientID: row.ClientID, UserID: row.UserID,
		EventName: row.EventName, CallbackUrl: row.CallbackUrl, SigningSecret: row.SigningSecret,
		PreviousSigningSecret: row.PreviousSigningSecret, PreviousSecretExpiresAt: row.PreviousSecretExpiresAt,
		Generation: row.Generation, SubscribedAt: row.SubscribedAt, ExpiresAt: row.ExpiresAt, VerifiedAt: row.VerifiedAt,
	}
	_, err = q.SaveSubscription(ctx, params)
	require.ErrorContains(t, err, "platform_mcp_event_subscriptions_project_fkey")
	params.ID, params.ProjectID = "cross-tenant-connection", row.ProjectID
	params.ConnectionID = conv.ToNullUUID(uuid.MustParse(other.ConnectionID))
	_, err = q.SaveSubscription(ctx, params)
	require.ErrorContains(t, err, "platform_mcp_event_subscriptions_connection_fkey")
	_, err = q.CreateDelivery(ctx, eventrepo.CreateDeliveryParams{
		ID: uuid.New(), OrganizationID: other.OrganizationID, ProjectID: otherProject.ID,
		SubscriptionID: conv.ToPGText(row.ID), SubscriptionGeneration: row.Generation,
		EventID: uuid.New(), EventName: row.EventName, RequestID: f.requestID, Payload: []byte(`{}`),
	})
	require.ErrorContains(t, err, "platform_mcp_event_deliveries_subscription_fkey")
}
