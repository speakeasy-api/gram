// Key resolution for the workload assertion grant: turning a trusted issuer
// row into the key source its assertions are verified against. Sibling of
// clientKeySource in authnchallenge_clientauth.go, which does the same job
// for a registered client's assertions.

package mcp

import (
	"fmt"

	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// workloadIssuerKeySource builds the key source a trusted workload issuer's
// assertions verify against.
//
// Only one shape exists here, unlike clientKeySource's two: a workload
// issuer publishes its keys and never registers them with us, so there is no
// inline key set to consider. The jwks_uri arrives on the row from the RFC
// 8414 / OIDC discovery the management API runs when an issuer is created or
// refreshed, which is why nothing is fetched or probed on this path.
//
// An issuer row with no jwks_uri is a setup error, not a bad assertion, and
// says so: discovery either never ran or the issuer's document omitted the
// field. Answering it like a rejected workload would send an operator
// hunting through their platform's configuration for a problem that is on
// ours.
func workloadIssuerKeySource(endpoint *ResolvedMcpEndpoint, issuer *remotesessions_repo.RemoteSessionIssuer) (jwks.Source, error) {
	if !issuer.JwksUri.Valid || issuer.JwksUri.String == "" {
		return jwks.Source{}, fmt.Errorf("trusted issuer %q records no jwks_uri: re-run discovery for it", issuer.Slug)
	}

	source, err := jwks.NewRemoteSource(issuer.JwksUri.String)
	if err != nil {
		return jwks.Source{}, fmt.Errorf("trusted issuer %q jwks_uri: %w", issuer.Slug, err)
	}

	return source.WithFetchScope(workloadFetchScope(endpoint)), nil
}

// workloadFetchScope names the budget a workload issuer's key fetches are
// charged to.
//
// The authorization server's own identifier is the tenant boundary the fetch
// limiter documents, so it is the base: one endpoint's trusted issuers
// cannot exhaust another's, and admitting more issuers buys no extra
// fetches. Never the issuer URL, which two organizations may legitimately
// share, and never a value derived from the request.
//
// The prefix keeps this grant's budget separate from the client
// authentication running against the same key resolver on the same
// endpoint. Client assertions only reach the resolver behind a registered
// client, while this grant is reachable by anyone, so an unauthenticated
// path must not be able to spend the budget an authenticated one depends
// on.
func workloadFetchScope(endpoint *ResolvedMcpEndpoint) string {
	return "workload:" + endpoint.UserSessionIssuerID.String()
}
