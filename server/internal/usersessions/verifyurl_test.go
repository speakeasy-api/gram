package usersessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The full outcome taxonomy is covered against live documents in
// internal/usersessions/cimd (inspect_test.go), where the resolver can be
// pointed at an httptest TLS server. These tests cover what the handler
// itself owns: authorization, and the mapping from an Inspection onto the
// wire result.

func TestVerifyURL_InvalidURLIsASuccessfulProbe(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// A syntactically dead URL is a probe RESULT, not a bad request: the
	// operator asked whether it works, and the answer is no.
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ClientIDMetadataURI: "http://client.example.com/client.json",
	})
	require.NoError(t, err)
	require.False(t, result.Verified)
	require.Equal(t, "invalid_url", result.Outcome)
	require.NotEmpty(t, result.Detail)
	require.Nil(t, result.HTTPStatus, "no response was received")
	require.NotNil(t, result.Reason)
	require.Equal(t, "client_id_scheme", *result.Reason)
	require.Nil(t, result.ClientName)
}

func TestVerifyURL_UnreachableHostIsASuccessfulProbe(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// RFC 5737 TEST-NET-1: syntactically valid, never routable.
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ClientIDMetadataURI: "https://192.0.2.1/oauth/client.json",
	})
	require.NoError(t, err, "an unreachable host is a probe outcome, not a handler error")
	require.False(t, result.Verified)
	require.Equal(t, "unreachable", result.Outcome)
	require.Nil(t, result.ClientName)
	// The detail must never echo the transport error, which names internal
	// network conditions.
	require.NotContains(t, result.Detail, "192.0.2.1")
	require.NotContains(t, result.Detail, "dial")
}

func TestVerifyURL_RateLimitedPerProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Comfortably past burst plus a minute's allowance. The exact constants
	// are unexported and this is an external test package, so assert the
	// property (it eventually refuses) rather than the precise cutoff. The
	// limiter is keyed per project and this test owns its own project, so
	// nothing else can spend the budget.
	var lastErr error
	for range 100 {
		_, lastErr = ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
			SessionToken:        nil,
			ApikeyToken:         nil,
			ProjectSlugInput:    nil,
			ClientIDMetadataURI: "http://client.example.com/client.json",
		})
		if lastErr != nil {
			break
		}
	}

	require.Error(t, lastErr, "the limiter must eventually refuse")
	requireOopsCode(t, lastErr, oops.CodeRateLimitExceeded)
}

func TestVerifyURL_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Hold only project:read; verify takes the write scope because it is a
	// pre-flight for create and makes Gram issue an outbound request.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	_, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		ClientIDMetadataURI: "https://client.example.com/client.json",
	})
	require.Error(t, err)
}
