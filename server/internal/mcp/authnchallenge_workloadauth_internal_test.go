package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

func workloadTestEndpoint(issuerID uuid.UUID) *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{UserSessionIssuerID: issuerID}
}

func workloadTestIssuer(name string, jwksURI string) *workloadidentity_repo.WorkloadIssuer {
	return &workloadidentity_repo.WorkloadIssuer{Name: name, JwksUri: jwksURI}
}

func TestWorkloadIssuerKeySource_BuildsRemoteSource(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	source, err := workloadIssuerKeySource(
		workloadTestEndpoint(issuerID),
		workloadTestIssuer("gh-actions", "https://example.test/keys"),
	)

	require.NoError(t, err)
	require.Equal(t, "https://example.test/keys", source.CacheKey(), "the cache key is the jwks_uri, shared across every scope naming it")
}

// Pins the guard, not an operator-facing path: jwks_uri is NOT NULL, so a row
// reaching here without one did not come from the table.
func TestWorkloadIssuerKeySource_EmptyJwksURIIsRefused(t *testing.T) {
	t.Parallel()

	_, err := workloadIssuerKeySource(
		workloadTestEndpoint(uuid.New()),
		workloadTestIssuer("gh-actions", ""),
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "gh-actions")
}

func TestWorkloadIssuerKeySource_RejectsUnusableJwksURI(t *testing.T) {
	t.Parallel()

	_, err := workloadIssuerKeySource(
		workloadTestEndpoint(uuid.New()),
		workloadTestIssuer("gh-actions", "http://example.test/keys"),
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
