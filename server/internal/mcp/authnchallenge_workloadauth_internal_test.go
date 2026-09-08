package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func workloadTestEndpoint(issuerID uuid.UUID) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{UserSessionIssuerID: issuerID}
}

func workloadTestIssuer(slug string, jwksURI pgtype.Text) *remotesessions_repo.RemoteSessionIssuer {
	return &remotesessions_repo.RemoteSessionIssuer{Slug: slug, JwksUri: jwksURI}
}

func TestWorkloadIssuerKeySource_BuildsRemoteSource(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	source, err := workloadIssuerKeySource(
		workloadTestEndpoint(issuerID),
		workloadTestIssuer("gh-actions", pgtype.Text{String: "https://example.test/keys", Valid: true}),
	)

	require.NoError(t, err)
	require.Equal(t, "https://example.test/keys", source.CacheKey(), "the cache key is the jwks_uri, shared across every scope naming it")
}

func TestWorkloadIssuerKeySource_MissingJwksURIIsASetupError(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		jwksURI pgtype.Text
	}{
		{name: "null", jwksURI: pgtype.Text{String: "", Valid: false}},
		{name: "empty", jwksURI: pgtype.Text{String: "", Valid: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := workloadIssuerKeySource(
				workloadTestEndpoint(uuid.New()),
				workloadTestIssuer("gh-actions", tt.jwksURI),
			)

			require.Error(t, err)
			// The message has to name the issuer and point at discovery: this
			// is our configuration gap, not the caller's assertion.
			require.ErrorContains(t, err, "gh-actions")
			require.ErrorContains(t, err, "re-run discovery")
		})
	}
}

func TestWorkloadIssuerKeySource_RejectsUnusableJwksURI(t *testing.T) {
	t.Parallel()

	_, err := workloadIssuerKeySource(
		workloadTestEndpoint(uuid.New()),
		workloadTestIssuer("gh-actions", pgtype.Text{String: "http://example.test/keys", Valid: true}),
	)

	require.Error(t, err, "plain http must not become a key source")
	require.ErrorContains(t, err, "gh-actions")
}

func TestWorkloadFetchScope_IsPerAuthorizationServerAndSeparateFromClientAuth(t *testing.T) {
	t.Parallel()

	first, second := uuid.New(), uuid.New()

	require.Equal(t, "workload:"+first.String(), workloadFetchScope(workloadTestEndpoint(first)),
		"keyed by the authorization server, prefixed so this grant's budget is separate from client auth's on the same endpoint")
	require.NotEqual(t, workloadFetchScope(workloadTestEndpoint(first)), workloadFetchScope(workloadTestEndpoint(second)),
		"one endpoint's issuers must not be able to exhaust another's budget")
}
