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

// workloadIssuerKeySource builds the key source a workload issuer's assertions
// verify against.
//
// One shape only, unlike clientKeySource's two: a workload issuer publishes its
// keys and never registers them with us. jwks_uri is stored on the row at
// discovery time, so nothing is fetched or probed here.
//
// jwks_uri is NOT NULL on workload_issuers, so the empty check guards a row
// that should not exist rather than an operator error. Errors name the issuer
// by name; a workload issuer has no slug, its URL being its canonical name.
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
