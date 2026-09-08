// Pins the DeleteMcpServer arm of the issuer-delete cascade: when the deleted
// server's issuer held a client's only live binding, the client's sessions are
// tombstoned in the same transaction and revoked upstream post-commit.

package mcpservers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDeleteMcpServer_RevokesOrphanedClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	backendID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "orphan revoke server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &backendID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.UserSessionIssuerID)
	issuerID := uuid.MustParse(*created.UserSessionIssuerID)

	var mu sync.Mutex
	var revokedTokens []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mu.Lock()
		revokedTokens = append(revokedTokens, form.Get("token"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	enc := testenv.NewEncryptionClient(t)
	q := remotesessionsrepo.New(ti.conn)

	remoteIssuer, err := q.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		Slug:                              "orphan-revoke-remote-issuer",
		Issuer:                            "https://orphan-revoke.example.com",
		Name:                              pgtype.Text{String: "", Valid: false},
		LogoAssetID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl:       pgtype.Text{String: "", Valid: false},
		AuthorizationEndpoint:             conv.ToPGText("https://orphan-revoke.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://orphan-revoke.example.com/token"),
		RevocationEndpoint:                conv.ToPGText(upstream.URL + "/revoke"),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ServiceDocumentation:              pgtype.Text{String: "", Valid: false},
		OpPolicyUri:                       pgtype.Text{String: "", Valid: false},
		OpTosUri:                          pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		ClientIDMetadataDocumentSupported: false,
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   remoteIssuer.ID,
		ClientID:                "orphan-revoke-cid",
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		TokenEndpointAuthMethod: conv.ToPGTextEmpty("none"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
		LegacyCallbackUrl:       false,
	})
	require.NoError(t, err)

	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   issuerID,
	}))

	accessEnc, err := enc.Encrypt([]byte("orphan-revoke-access"))
	require.NoError(t, err)
	refreshEnc, err := enc.Encrypt([]byte("orphan-revoke-refresh"))
	require.NoError(t, err)

	_, err = q.UpsertRemoteSession(ctx, remotesessionsrepo.UpsertRemoteSessionParams{
		SubjectUrn:             urn.NewUserSubject("orphan-revoke-subject"),
		UserSessionIssuerID:    issuerID,
		RemoteSessionClientID:  client.ID,
		AccessTokenEncrypted:   accessEnc,
		AccessExpiresAt:        conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted:  conv.ToPGText(refreshEnc),
		AuthorizationExpiresAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Scopes:                 []string{},
		Resource:               pgtype.Text{String: "", Valid: false},
		AutoRefresh:            false,
	})
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	active, err := q.CountActiveRemoteSessionsByClientID(ctx, client.ID)
	require.NoError(t, err)
	require.Zero(t, active, "the orphaned client's sessions are tombstoned with the server's issuer")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"orphan-revoke-refresh"}, revokedTokens, "the stored refresh token is revoked upstream")
}
