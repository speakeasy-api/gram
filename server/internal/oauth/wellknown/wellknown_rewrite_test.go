package wellknown

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// TestRewriteMetadataIssuer_Object rewrites the top-level issuer while leaving
// every other field of the captured upstream document untouched.
func TestRewriteMetadataIssuer_Object(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"issuer":"https://upstream.example.com","token_endpoint":"https://upstream.example.com/token"}`)
	out, err := rewriteMetadataIssuer(raw, "https://gram.example.com/mcp/foo")
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, "https://gram.example.com/mcp/foo", got["issuer"])
	require.Equal(t, "https://upstream.example.com/token", got["token_endpoint"])
}

type issuerOnlyOAuthRepo struct{}

func (issuerOnlyOAuthRepo) GetExternalOAuthServerMetadata(context.Context, oauthrepo.GetExternalOAuthServerMetadataParams) (oauthrepo.ExternalOauthServerMetadatum, error) {
	return oauthrepo.ExternalOauthServerMetadatum{AuthorizationServerIssuer: pgtype.Text{String: "https://auth.example.com", Valid: true}}, nil
}

func TestResolveOAuthServerMetadataFromToolset_IssuerOnlyIsNotFound(t *testing.T) {
	t.Parallel()

	result, err := ResolveOAuthServerMetadataFromToolset(t.Context(), nil, nil, issuerOnlyOAuthRepo{}, nil, &toolsetsrepo.Toolset{
		ProjectID: uuid.New(), ExternalOauthServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}, "", "", "https://gram.example.com/mcp/foo")
	require.NoError(t, err)
	require.Nil(t, result)
}

// TestRewriteMetadataIssuer_NullPayload verifies a JSON null document, which
// unmarshals into a nil map, returns an error rather than panicking on the
// issuer assignment.
func TestRewriteMetadataIssuer_NullPayload(t *testing.T) {
	t.Parallel()

	_, err := rewriteMetadataIssuer(json.RawMessage("null"), "https://gram.example.com/mcp/foo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected a JSON object")
}
