package assistanttokens

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var tokensInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch assistanttokens test infrastructure: %v", err)
	}
	tokensInfra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup assistanttokens test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	conn        *pgxpool.Pool
	manager     *Manager
	projectID   uuid.UUID
	assistantID uuid.UUID
	agentID     uuid.UUID
	threadID    uuid.UUID
	chatID      uuid.UUID
	userID      string
}

func newFixture(t *testing.T, dbName string) fixture {
	t.Helper()

	conn, err := tokensInfra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	ctx := t.Context()
	userID := "user-test"

	_, err = usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       "user@example.test",
		DisplayName: "Test User",
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:   "org-test",
		Name: "Test Org",
		Slug: "org-test",
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: "org-test",
		UserID:         pgtype.Text{String: userID, Valid: true},
	})
	require.NoError(t, err)

	proj, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeProjectRead, proj.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: "org-test",
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)

	agent, err := agentrepo.New(conn).CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: "org-test",
		OwnerUserID:    userID,
		ProjectID:      uuid.NullUUID{UUID: proj.ID, Valid: true},
		Name:           "Assistant Agent",
	})
	require.NoError(t, err)

	assistant, err := assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       proj.ID,
		OrganizationID:  "org-test",
		CreatedByUserID: pgtype.Text{String: userID, Valid: true},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)

	chatID := uuid.New()
	err = assistantsrepo.New(conn).UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      proj.ID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	})
	require.NoError(t, err)

	threadID, err := assistantsrepo.New(conn).UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistant.ID,
		ProjectID:     proj.ID,
		CorrelationID: "corr-1",
		ChatID:        chatID,
		SourceKind:    "slack",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
		"https://platform.example.invalid",
		false,
	)
	require.NoError(t, err)

	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	engine := authz.NewEngine(
		testenv.NewLogger(t), conn, authztest.ChallengeLoggingAlwaysDisabled, nil,
		authz.EngineOpts{
			AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential,
			AdmitWorkloadSession:     runtimepolicy.AdmitWorkloadSession,
		},
	)
	manager := New("test-secret", conn, engine, principalcredential.New(signer, conn), cache.NewRedisCacheAdapter(redisClient))

	return fixture{
		conn:        conn,
		manager:     manager,
		projectID:   proj.ID,
		assistantID: assistant.ID,
		agentID:     agent.ID,
		threadID:    threadID,
		chatID:      chatID,
		userID:      userID,
	}
}

func (f fixture) mintCredential(t *testing.T) string {
	t.Helper()
	token, err := f.manager.MintTurnCredential(t.Context(), principalcredential.Credential{
		OrganizationID:   "org-test",
		ProjectID:        f.projectID,
		Principal:        urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()),
		AuthorizerUserID: f.userID,
	}, RuntimeBinding{
		OrganizationID: "org-test",
		ProjectID:      f.projectID,
		AssistantID:    f.assistantID,
		ThreadID:       f.threadID,
		ChatID:         f.chatID,
	})
	require.NoError(t, err)
	return token
}

func TestAuthorizeRuntime_ActiveAndLifecycleRevocation(t *testing.T) {
	t.Parallel()

	t.Run("active", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "tokens_runtime_active")
		token := f.mintCredential(t)
		authed, claims, err := f.manager.AuthorizeRuntime(t.Context(), "Bearer "+token)
		require.NoError(t, err)
		require.Equal(t, f.assistantID.String(), claims.AssistantID)
		require.Equal(t, f.threadID.String(), claims.ThreadID)
		require.Equal(t, f.userID, claims.UserID)
		chatID, bound := RuntimeChatID(authed)
		require.True(t, bound)
		require.Equal(t, f.chatID, chatID)
	})

	t.Run("thread deleted", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "tokens_runtime_thread_deleted")
		token := f.mintCredential(t)
		err := assistantsrepo.New(f.conn).SoftDeleteAssistantThread(t.Context(), assistantsrepo.SoftDeleteAssistantThreadParams{
			ID:        f.threadID,
			ProjectID: f.projectID,
		})
		require.NoError(t, err)
		_, _, err = f.manager.AuthorizeRuntime(t.Context(), token)
		requireUnauthorized(t, err)
	})

	t.Run("assistant deleted", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "tokens_runtime_assistant_deleted")
		token := f.mintCredential(t)
		err := assistantsrepo.New(f.conn).DeleteAssistant(t.Context(), assistantsrepo.DeleteAssistantParams{
			AssistantID: f.assistantID,
			ProjectID:   f.projectID,
		})
		require.NoError(t, err)
		_, _, err = f.manager.AuthorizeRuntime(t.Context(), token)
		requireUnauthorized(t, err)
	})

	t.Run("assistant paused", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "tokens_runtime_assistant_paused")
		token := f.mintCredential(t)
		err := assistantsrepo.New(f.conn).SetAssistantStatus(t.Context(), assistantsrepo.SetAssistantStatusParams{
			Status:    "paused",
			ID:        f.assistantID,
			ProjectID: f.projectID,
		})
		require.NoError(t, err)
		_, _, err = f.manager.AuthorizeRuntime(t.Context(), token)
		requireUnauthorized(t, err)
	})
}

func TestAuthorizeRuntime_RejectsNonPrincipalCredentialTokens(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tokens_runtime_reject_non_principal")
	signedJWT, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("test-secret"))
	require.NoError(t, err)

	for _, header := range []string{"Bearer " + signedJWT, "Bearer not-a-credential"} {
		_, _, err = f.manager.AuthorizeRuntime(t.Context(), header)
		requireUnauthorized(t, err)
	}
}

func TestMCPAuthFlowSeparatesStableCallbackFromAttempt(t *testing.T) {
	t.Parallel()

	manager := New("test-secret", nil, nil, nil, nil)
	assistantID := uuid.New()
	attemptID := uuid.NewString()
	token, err := manager.GenerateMCPAuthFlow(MCPAuthFlowInput{
		OrgID:             "org-test",
		ProjectID:         uuid.New(),
		UserID:            "user-test",
		AssistantID:       assistantID,
		ThreadID:          uuid.New(),
		AttemptID:         attemptID,
		FlowID:            assistantID.String(),
		ServerID:          "server-test",
		McpURL:            "https://mcp.example.com/mcp/test",
		ClientID:          "client-test",
		ClientSecret:      "encrypted-secret",
		RedirectURI:       "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback",
		CodeVerifier:      "encrypted-verifier",
		TokenEndpoint:     "https://auth.example.com/token",
		OAuthServerIssuer: "https://auth.example.com",
		TTL:               time.Minute,
	})
	require.NoError(t, err)

	claims, err := manager.ValidateMCPAuthFlow(token)
	require.NoError(t, err)
	require.Equal(t, assistantID.String(), claims.FlowID)
	require.Equal(t, attemptID, claims.ID)
	require.Equal(t, "https://auth.example.com", claims.OAuthServerIssuer)
}

func TestMCPAuthFlowDefaultsAttemptToFlowID(t *testing.T) {
	t.Parallel()

	manager := New("test-secret", nil, nil, nil, nil)
	flowID := uuid.NewString()
	token, err := manager.GenerateMCPAuthFlow(MCPAuthFlowInput{
		OrgID:             "org-test",
		ProjectID:         uuid.New(),
		UserID:            "user-test",
		AssistantID:       uuid.New(),
		ThreadID:          uuid.New(),
		AttemptID:         "",
		FlowID:            flowID,
		ServerID:          "server-test",
		McpURL:            "https://mcp.example.com/mcp/test",
		ClientID:          "client-test",
		ClientSecret:      "encrypted-secret",
		RedirectURI:       "https://gram.example.com/rpc/assistantMcpAuth/" + flowID + "/oauth/callback",
		CodeVerifier:      "encrypted-verifier",
		TokenEndpoint:     "https://auth.example.com/token",
		OAuthServerIssuer: "",
		TTL:               time.Minute,
	})
	require.NoError(t, err)

	claims, err := manager.ValidateMCPAuthFlow(token)
	require.NoError(t, err)
	require.Equal(t, flowID, claims.FlowID)
	require.Equal(t, flowID, claims.ID)
}

func requireUnauthorized(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	se := &oops.ShareableError{}
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeUnauthorized, se.Code)
}
