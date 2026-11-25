package chat_test

import (
	"context"
	"encoding/json"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"goa.design/goa/v3/security"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/billing"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestCompletionKitWorkloadAdmission(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := t.Context()
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: "test-owner", Email: "owner@example.test", DisplayName: "Test owner"})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: ti.orgID, UserID: pgtype.Text{String: "test-owner", Valid: true}})
	require.NoError(t, err)
	assistantID := seedAssistant(t, ctx, ti)
	err = assistantsrepo.New(ti.conn).SetAssistantDesiredRuntimeVersionForTest(ctx, assistantsrepo.SetAssistantDesiredRuntimeVersionForTestParams{AssistantID: assistantID, ProjectID: ti.projectID, RuntimeVersion: 3})
	require.NoError(t, err)
	_, err = triggerrepo.New(ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, DefinitionSlug: "slack", Name: "Test trigger", TargetKind: "assistant", TargetRef: assistantID.String(), TargetDisplay: "Assistant", ConfigJson: []byte(`{}`), Status: "active",
	})
	require.NoError(t, err)
	identities := assistantidentity.New("https://example.test", audit.NewLogger())
	require.NoError(t, pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: ti.orgID, ProjectID: ti.projectID, AssistantID: assistantID, ActorUserID: "test-owner"}))
		return nil
	}))
	resolved, err := identities.ResolveRuntime(ctx, ti.conn, ti.orgID, ti.projectID, assistantID)
	require.NoError(t, err)
	runtimeID := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	require.NoError(t, assistantsrepo.New(ti.conn).CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
		ID: runtimeID, AssistantID: assistantID, ProjectID: ti.projectID, Backend: "local", BackendMetadataJson: []byte(`{}`), State: "active", WarmUntil: now, LastHeartbeatAt: now, UpdatedAt: now,
	}))
	err = chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3, EndedAt: pgtype.Timestamptz{}})
	require.NoError(t, err)
	tokens := ti.service.TestingCompletionTokens()
	// Historical credentials must never survive as ordinary consumer API keys.
	for _, revoked := range []bool{false, true} {
		raw, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
		require.NoError(t, err)
		key, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{OrganizationID: ti.orgID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, CreatedByUserID: "test-owner", Name: "historical-runtime-" + uuid.NewString(), KeyPrefix: prefix, KeyHash: hash, Scopes: []string{"consumer"}})
		require.NoError(t, err)
		scheme := &security.APIKeyScheme{Name: "gram-key", RequiredScopes: []string{"consumer"}}
		// Use the actual shared key boundary, not completion-specific auth.
		boundary := auth.NewKeyAuth(ti.conn, testenv.NewLogger(t), billing.NewStubClient(testenv.NewLogger(t), testenv.NewTracerProvider(t)))
		_, err = boundary.KeyBasedAuth(ctx, raw, scheme.RequiredScopes)
		require.NoError(t, err, "ordinary keys remain supported")
		policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.AssistantIssuedPolicyVersion, nil)
		require.NoError(t, err)
		policyJSON, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.AssistantIssuedPolicyVersion, policy)
		require.NoError(t, err)
		require.NoError(t, chatrepo.New(ti.conn).SetHistoricalKitKeyProfileFixture(ctx, chatrepo.SetHistoricalKitKeyProfileFixtureParams{ApiKeyID: key.ID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, SubjectUrn: conv.ToPGText(urn.NewPrincipal(urn.PrincipalTypeAgent, resolved.AgentID.String()).String()), DelegatedGrants: policyJSON, DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.AssistantIssuedPolicyVersion), Valid: true}}))
		_, err = boundary.KeyBasedAuth(ctx, raw, scheme.RequiredScopes)
		require.NoError(t, err, "the former Kit principal profile is valid until identified as superseded")

		revokedAt := pgtype.Timestamptz{}
		if revoked {
			revokedAt = now
		}
		require.NoError(t, chatrepo.New(ti.conn).InsertSupersededKitKeyFixture(ctx, chatrepo.InsertSupersededKitKeyFixtureParams{ApiKeyID: key.ID, ProjectID: ti.projectID, AssistantID: assistantID, RuntimeID: runtimeID, RevokedAt: revokedAt}))
		for _, ended := range []pgtype.Timestamptz{{}, now} {
			require.NoError(t, chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3, EndedAt: ended}))
			_, err = boundary.KeyBasedAuth(ctx, raw, scheme.RequiredScopes)
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, oops.CodeUnauthorized, denied.Code, "active and retired legacy runtime keys fail closed")
		}
	}
	require.NoError(t, chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3}))

	issuerID := uuid.New()
	subject := "projected-runtime-generation"
	metadata, err := json.Marshal(map[string]any{"workload": map[string]any{"issuer_id": issuerID, "subject": subject, "identity": resolved}})
	require.NoError(t, err)
	require.NoError(t, assistantsrepo.New(ti.conn).UpdateAssistantRuntimeMetadata(ctx, assistantsrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: ti.projectID, RuntimeID: runtimeID, BackendMetadataJson: metadata}))
	effectiveAgent := resolved.AgentID
	ti.service.WithWorkloadInference(kitWorkloadAuthorizer(func(ctx context.Context, token string) (context.Context, bool, error) {
		require.Equal(t, "verified-workload", token)
		ac := &contextvalues.AuthContext{ActiveOrganizationID: ti.orgID, ProjectID: &ti.projectID}
		ctx = contextvalues.WithAuthenticatedActor(ctx, ac, urn.NewWorkloadPrincipal(issuerID, subject))
		ctx = contextvalues.WithWorkloadAgent(ctx, urn.NewPrincipal(urn.PrincipalTypeAgent, effectiveAgent.String()))
		return ctx, true, nil
	}))
	request := func() *http.Request {
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/chat/completions", nil)
		r.Header.Set("Authorization", "Bearer verified-workload")
		return r
	}
	authorized, ac, source, err := ti.service.TestingAuthorizeCompletion(request(), true)
	require.NoError(t, err)
	require.Equal(t, ti.projectID, *ac.ProjectID)
	require.Equal(t, billing.ModelUsageSourceAssistants, source)
	require.True(t, assistanttokens.InferenceTranscriptOwned(authorized))
	_, bound := assistanttokens.RuntimeChatID(authorized)
	require.False(t, bound, "the request body must select a persisted ACP session")
	effectiveAgent = uuid.New()
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(), true)
	requireKitAuthError(t, err, oops.CodeUnauthorized)
	effectiveAgent = resolved.AgentID
	for _, mismatch := range []string{"issuer", "subject"} {
		savedIssuer, savedSubject := issuerID, subject
		if mismatch == "issuer" {
			issuerID = uuid.New()
		} else {
			subject = "unrelated-workload"
		}
		ordinary, _, slot, err := ti.service.TestingAuthorizeCompletion(request(), true)
		require.NoError(t, err)
		require.Equal(t, billing.ModelUsageSourcePlayground, slot)
		require.False(t, assistanttokens.InferenceTranscriptOwned(ordinary), "unrelated workload cannot claim managed attribution")
		issuerID, subject = savedIssuer, savedSubject
	}

	for _, field := range []string{"identity project", "identity assistant", "identity binding", "identity generation"} {
		changed := resolved
		switch field {
		case "identity project":
			changed.ProjectID = uuid.New()
		case "identity assistant":
			changed.AssistantID = uuid.New()
		case "identity binding":
			changed.BindingID = uuid.New()
		case "identity generation":
			changed.BindingGeneration++
		}
		invalid, err := json.Marshal(map[string]any{"workload": map[string]any{"issuer_id": issuerID, "subject": subject, "identity": changed}})
		require.NoError(t, err)
		require.NoError(t, assistantsrepo.New(ti.conn).UpdateAssistantRuntimeMetadata(ctx, assistantsrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: ti.projectID, RuntimeID: runtimeID, BackendMetadataJson: invalid}))
		_, _, _, err = ti.service.TestingAuthorizeCompletion(request(), true)
		requireKitAuthError(t, err, oops.CodeUnauthorized)
	}
	require.NoError(t, assistantsrepo.New(ti.conn).UpdateAssistantRuntimeMetadata(ctx, assistantsrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: ti.projectID, RuntimeID: runtimeID, BackendMetadataJson: metadata}))
	_, err = tokens.BindKitSession(authorized, uuid.New())
	requireKitAuthError(t, err, oops.CodeForbidden)
	for _, header := range []string{"Gram-Chat-ID", "Gram-Assistant-ID"} {
		r := request()
		r.Header.Set(header, uuid.NewString())
		_, _, _, err = ti.service.TestingAuthorizeCompletion(r, true)
		requireKitAuthError(t, err, oops.CodeForbidden)
	}
	require.NoError(t, chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3, EndedAt: now}))
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(), true)
	requireKitAuthError(t, err, oops.CodeUnauthorized)
	require.NoError(t, chatrepo.New(ti.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: runtimeID, ProjectID: ti.projectID, RuntimeVersion: 3}))
	_, err = agentrepo.New(ti.conn).SuspendAgent(ctx, agentrepo.SuspendAgentParams{OrganizationID: ti.orgID, ID: resolved.AgentID})
	require.NoError(t, err)
	_, _, _, err = ti.service.TestingAuthorizeCompletion(request(), true)
	requireKitAuthError(t, err, oops.CodeUnauthorized)
}

type kitWorkloadAuthorizer func(context.Context, string) (context.Context, bool, error)

func (f kitWorkloadAuthorizer) AuthorizeWorkloadInference(ctx context.Context, token string) (context.Context, bool, error) {
	return f(ctx, token)
}

func requireKitAuthError(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var denied *oops.ShareableError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, code, denied.Code)
}
