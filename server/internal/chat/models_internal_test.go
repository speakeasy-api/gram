package chat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandleModelsRejectsInvalidRuntimeCredentialWithoutFallback(t *testing.T) {
	t.Parallel()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
	token.Header["typ"] = principalcredential.TokenType
	raw, err := token.SignedString([]byte("invalid-runtime-signing-key"))
	require.NoError(t, err)
	require.True(t, assistanttokens.IsRuntimeCredential("Bearer "+raw))

	// The runtime verifier requires RS256. No session/API-key fallback is wired:
	// attempting that path instead of refusing the credential would panic.
	svc := &Service{
		assistantTokens: assistanttokens.New("", nil, nil, principalcredential.New(&mcpauthz.Issuer{}, nil), nil),
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/chat/models", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	oops.ErrHandle(testenv.NewLogger(t), svc.HandleModels).ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"data"`)
}
