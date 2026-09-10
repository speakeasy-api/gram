// Key resolution for the workload assertion grant: turning a trusted issuer
// row into the key source its assertions are verified against. Sibling of
// clientKeySource in authnchallenge_clientauth.go, which does the same job
// for a registered client's assertions.

package mcp

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
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
// The row comes from workloadidentity.ResolveIssuerByURL, where jwks_uri is
// NOT NULL by construction: a workload issuer that can verify nothing must
// not be storable. The empty check below is therefore a guard against a row
// that should not exist rather than an operator error to explain, and names
// the issuer by name because that is the identifier an operator works with.
// A workload issuer has no slug: its issuer URL is already its canonical
// machine-readable name.
func workloadIssuerKeySource(endpoint *ResolvedMcpEndpoint, issuer *workloadidentity_repo.WorkloadIssuer) (jwks.Source, error) {
	if issuer.JwksUri == "" {
		return jwks.Source{}, fmt.Errorf("workload issuer %q records no jwks_uri", issuer.Name)
	}

	source, err := jwks.NewRemoteSource(issuer.JwksUri)
	if err != nil {
		return jwks.Source{}, fmt.Errorf("workload issuer %q jwks_uri: %w", issuer.Name, err)
	}

	return source.WithFetchScope(workloadFetchScope(endpoint)), nil
}

// workloadFetchScope names the budget a workload issuer's key fetches are
// charged to.
//
// The authorization server's own identifier is the tenant boundary the fetch
// limiter documents, and every issuer trusted on that endpoint shares the one
// budget deliberately. The limiter exists so that no number of registrations
// buys more fetches; keying per issuer row would hand an operator a fresh
// budget for each issuer they add, which is the amplification it closes. One
// tenant's issuers contending for that tenant's own budget is the accepted
// trade. Never the issuer URL, which two organizations may legitimately
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
