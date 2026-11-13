package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type ateIssuanceGate struct {
	enabled atomic.Bool
	query   string
	entered chan struct{}
	release chan struct{}
}

func (g *ateIssuanceGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, g.query) && g.enabled.CompareAndSwap(true, false) {
		close(g.entered)
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*ateIssuanceGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Use the real issuer signature, JWT exchange, workload policy and issuance
// transaction. Only the database query gate delays the grant for race assertions.
func newATEIssuanceFixture(t *testing.T, gate *ateIssuanceGate) (workloadGrantFixture, assistantrepo.GetAssistantRuntimeV2Row, func(string, string) string) {
	t.Helper()
	f := newWorkloadGrantFixtureWithPool(t, testenv.NewLogger(t), "", func(c *pgxpool.Config) { c.ConnConfig.Tracer = gate })
	ctx := t.Context()
	project := f.fx.target.ProjectID
	q := assistantrepo.New(f.ti.conn)
	a, err := q.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{ProjectID: project, OrganizationID: f.fx.orgID, CreatedByUserID: conv.ToPGText(f.fx.userID), Name: "ATE issuance", Model: "test/model", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
	require.NoError(t, err)
	_, err = triggerrepo.New(f.ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{OrganizationID: f.fx.orgID, ProjectID: project, DefinitionSlug: "manual", Name: "ATE root", TargetKind: "assistant", TargetRef: a.ID.String(), TargetDisplay: a.Name, ConfigJson: []byte(`{}`), Status: "active"})
	require.NoError(t, err)
	identities := assistantidentity.New(f.ti.serverURL.String(), f.ti.audit)
	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: f.fx.orgID, ProjectID: project, AssistantID: a.ID, ActorUserID: f.fx.userID}))
	require.NoError(t, tx.Commit(ctx))
	identity, err := identities.ResolveRuntime(ctx, f.ti.conn, f.fx.orgID, project, a.ID)
	require.NoError(t, err)
	chatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: chatID, ProjectID: project, OrganizationID: f.fx.orgID, UserID: conv.ToPGText(f.fx.userID)}))
	thread, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: a.ID, ProjectID: project, CorrelationID: "ate-issuance", ChatID: chatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: a.ID, RuntimeVersion: 3}))
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: a.ID, AssistantThreadID: thread, Backend: "ate", State: "active"}))
	runtime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: a.ID})
	require.NoError(t, err)
	actor := "spk-kit-" + strings.ReplaceAll(runtime.ID.String(), "-", "") + "-0123456789abcdef"
	f.subject = "actor/preview/" + actor
	f.agentID = identity.AgentID
	f.assignmentID = assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, f.issuerID, f.subject, f.agentID)
	require.NoError(t, testrepo.New(f.ti.conn).CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{OrganizationID: f.fx.orgID, ProjectID: uuid.NullUUID{}, WorkloadIssuerID: f.issuerID, Subject: f.subject}))
	seedPrincipalMCPConnectGrant(t, ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), f.fx.target.MCPResourceID)
	enableWorkloadInference(t, f)
	metadata, err := json.Marshal(map[string]any{"runtime_id": runtime.ID, "assistant_id": a.ID, "project_id": project, "atespace": "preview", "threads": map[string]any{thread.String(): map[string]any{"actor_name": actor, "actor_uid": "original-uid", "workload": map[string]any{"issuer_id": f.issuerID, "subject": f.subject, "identity": identity}}}})
	require.NoError(t, err)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: project, RuntimeID: runtime.ID, BackendMetadataJson: metadata}))
	mint := func(audience, uid string) string {
		t.Helper()
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.issuer.SigningKey()}, (&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), f.issuer.KeyID()))
		require.NoError(t, err)
		raw, err := jwt.Signed(signer).Claims(oauthtest.WorkloadClaims(f.issuer, f.subject, audience)).Claims(map[string]any{"actor.ate.dev": map[string]string{"atespace": "preview", "actorName": actor, "actorUID": uid}}).Serialize()
		require.NoError(t, err)
		return raw
	}
	return f, runtime, mint
}

func TestATEGrantSignedActorBinding(t *testing.T) {
	t.Parallel()
	for _, inference := range []bool{false, true} {
		t.Run(fmt.Sprintf("inference=%t", inference), func(t *testing.T) {
			t.Parallel()
			gate := &ateIssuanceGate{query: "unused", entered: make(chan struct{}), release: make(chan struct{})}
			f, runtime, mint := newATEIssuanceFixture(t, gate)
			audience := f.advertisedIssuer
			if inference {
				audience = f.sharedIssuer(t)
			}
			exchange := func(uid string) *httptest.ResponseRecorder {
				if inference {
					return exchangeWorkloadInference(t, f, mint(audience, uid))
				}
				return f.exchange(t, mint(audience, uid), f.resource)
			}
			requireWorkloadGrantRefused(t, exchange("replacement-uid"))
			requireWorkloadGrantRefused(t, exchange(""))
			w := exchange("original-uid")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NoError(t, assistantrepo.New(f.ti.conn).UpdateAssistantRuntimeMetadata(t.Context(), assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: runtime.ProjectID, RuntimeID: runtime.ID, BackendMetadataJson: []byte(`{}`)}))
			requireWorkloadGrantRefused(t, exchange("original-uid"))
		})
	}
}

func startATEGrantExchange(t *testing.T, f workloadGrantFixture, raw string) (*httptest.ResponseRecorder, <-chan error) {
	t.Helper()
	form := url.Values{"grant_type": {workloadGrantJWTBearer}, "assertion": {raw}, "resource": {f.resource}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.fx.toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", f.fx.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, route))
	w := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { done <- f.ti.service.HandleToken(w, req) }()
	return w, done
}

func TestATEGrantRechecksAfterLifecycleWait(t *testing.T) {
	t.Parallel()
	for _, withdrawAssignment := range []bool{false, true} {
		t.Run(fmt.Sprintf("withdraw=%t", withdrawAssignment), func(t *testing.T) {
			t.Parallel()
			gate := &ateIssuanceGate{query: "-- name: AcquireAssistantAdvisoryLock", entered: make(chan struct{}), release: make(chan struct{})}
			f, runtime, mint := newATEIssuanceFixture(t, gate)
			raw := mint(f.advertisedIssuer, "original-uid")
			gate.enabled.Store(true)
			w, done := startATEGrantExchange(t, f, raw)
			select {
			case <-gate.entered:
			case <-time.After(20 * time.Second):
				close(gate.release)
				t.Fatal("grant did not reach lifecycle fence")
			}
			if withdrawAssignment {
				unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
			} else {
				require.NoError(t, assistantrepo.New(f.ti.conn).EndAssistantRuntimeForTest(t.Context(), assistantrepo.EndAssistantRuntimeForTestParams{ProjectID: runtime.ProjectID, RuntimeID: runtime.ID}))
			}
			close(gate.release)
			select {
			case err := <-done:
				require.NoError(t, err)
				requireWorkloadGrantRefused(t, w)
			case <-time.After(20 * time.Second):
				t.Fatal("grant did not finish")
			}
			require.Empty(t, f.workloadSessions(t))
		})
	}
}

func TestATEGrantHoldsLifecycleFencesThroughIssuance(t *testing.T) {
	t.Parallel()
	gate := &ateIssuanceGate{query: "-- name: CreateUserSession :one", entered: make(chan struct{}), release: make(chan struct{})}
	f, runtime, mint := newATEIssuanceFixture(t, gate)
	raw := mint(f.advertisedIssuer, "original-uid")
	gate.enabled.Store(true)
	w, done := startATEGrantExchange(t, f, raw)
	select {
	case <-gate.entered:
	case <-time.After(20 * time.Second):
		close(gate.release)
		t.Fatal("grant never reached session insert")
	}
	for _, write := range []func(context.Context, pgx.Tx) error{
		func(ctx context.Context, tx pgx.Tx) error {
			return assistantrepo.New(tx).EndAssistantRuntimeForTest(ctx, assistantrepo.EndAssistantRuntimeForTestParams{RuntimeID: runtime.ID, ProjectID: runtime.ProjectID})
		},
		func(ctx context.Context, tx pgx.Tx) error {
			return assistantrepo.New(tx).SetAssistantStatus(ctx, assistantrepo.SetAssistantStatusParams{Status: "suspended", ID: runtime.AssistantID, ProjectID: runtime.ProjectID})
		},
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := agentrepo.New(tx).SuspendAgent(ctx, agentrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
			if err != nil {
				return fmt.Errorf("suspend fenced agent: %w", err)
			}
			return nil
		},
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := workloadrepo.New(tx).LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: f.fx.orgID, ID: f.issuerID})
			if err != nil {
				return fmt.Errorf("lock fenced issuer: %w", err)
			}
			return nil
		},
	} {
		tx := testenv.BeginTx(t, t.Context(), f.ti.conn)
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		err := write(ctx, tx)
		cancel()
		// Query cancellation may already have closed the writer connection.
		_ = tx.Rollback(t.Context())
		if err == nil {
			close(gate.release)
			t.Fatal("lifecycle writer bypassed grant fence")
		}
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}

	close(gate.release)
	select {
	case err := <-done:
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	case <-time.After(20 * time.Second):
		t.Fatal("grant did not finish")
	}
}
