package assistanttokens

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newTestPrincipalCredentialIssuer(t *testing.T, f fixture) *principalcredential.Issuer {
	t.Helper()
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
	_, err = orgrepo.New(f.conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:             "org-test",
		Slug:           "org-test",
		Name:           "Test Org",
		WorkosID:       pgtype.Text{},
		Whitelisted:    pgtype.Bool{},
		CreationSource: pgtype.Text{},
	})
	require.NoError(t, err)
	return principalcredential.New(signer, f.conn)
}

func TestBindRuntime(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "bind_runtime")
	redisClient, err := tokensInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	credentials := newTestPrincipalCredentialIssuer(t, f)
	manager := New("test-jwt-secret", f.conn, nil, credentials, cache.NewRedisCacheAdapter(redisClient))

	workload := principalcredential.Credential{
		OrganizationID: "org-test",
		ProjectID:      f.projectID,
		Principal:      urn.NewWorkloadPrincipal(uuid.New(), "trigger:1"),
	}
	binding := RuntimeBinding{
		OrganizationID: "org-test",
		ProjectID:      f.projectID,
		AssistantID:    f.assistantID,
		ThreadID:       f.threadID,
		ChatID:         f.chatID,
	}

	// A bare principal credential with no runtime binding stays unbound without error.
	bareRaw, _, err := credentials.Mint(workload)
	require.NoError(t, err)
	bareCtx, err := credentials.Authenticate(t.Context(), bareRaw)
	require.NoError(t, err)
	boundBare, err := manager.BindRuntime(bareCtx)
	require.NoError(t, err)
	_, hasPrincipal := contextvalues.GetAssistantPrincipal(boundBare)
	require.False(t, hasPrincipal)
	_, hasChat := RuntimeChatID(boundBare)
	require.False(t, hasChat)

	// A turn credential binds its assistant, thread, and chat.
	turnRaw, err := manager.MintTurnCredential(t.Context(), workload, binding)
	require.NoError(t, err)
	turnCtx, err := credentials.Authenticate(t.Context(), turnRaw)
	require.NoError(t, err)
	boundTurn, err := manager.BindRuntime(turnCtx)
	require.NoError(t, err)
	gotPrincipal, hasPrincipal := contextvalues.GetAssistantPrincipal(boundTurn)
	require.True(t, hasPrincipal)
	require.Equal(t, contextvalues.AssistantPrincipal{AssistantID: f.assistantID, ThreadID: f.threadID}, gotPrincipal)
	gotChat, hasChat := RuntimeChatID(boundTurn)
	require.True(t, hasChat)
	require.Equal(t, f.chatID, gotChat)
	require.False(t, InferenceTranscriptOwned(boundTurn))

	// A Kit-owned turn credential minted with a short lifetime binds the runtime
	// and marks the transcript as Kit-owned while keeping RuntimeChatID intact.
	kitBinding := binding
	kitBinding.TranscriptOwned = true
	kitRaw, kitExp, err := manager.MintTurnCredentialWithLifetime(t.Context(), workload, kitBinding, 5*time.Minute)
	require.NoError(t, err)
	require.True(t, kitExp.After(time.Now().Add(4*time.Minute)))
	kitCtx, err := credentials.Authenticate(t.Context(), kitRaw)
	require.NoError(t, err)
	boundKit, err := manager.BindRuntime(kitCtx)
	require.NoError(t, err)
	require.True(t, InferenceTranscriptOwned(boundKit))
	gotKitChat, hasKitChat := RuntimeChatID(boundKit)
	require.True(t, hasKitChat)
	require.Equal(t, f.chatID, gotKitChat)

	// Soft-deleting the bound thread rejects the turn credential as unauthorized.
	require.NoError(t, assistantsrepo.New(f.conn).SoftDeleteAssistantThread(t.Context(), assistantsrepo.SoftDeleteAssistantThreadParams{
		ID:        f.threadID,
		ProjectID: f.projectID,
	}))
	_, err = manager.BindRuntime(turnCtx)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)

	// A context with no principal credential is a no-op.
	plain, err := manager.BindRuntime(t.Context())
	require.NoError(t, err)
	_, hasPrincipal = contextvalues.GetAssistantPrincipal(plain)
	require.False(t, hasPrincipal)
}
