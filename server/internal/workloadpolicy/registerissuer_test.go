package workloadpolicy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func registerAnthropic(t *testing.T, ctx context.Context, ti *testInstance, allowWildcard bool) *gen.WorkloadIdentityPolicy {
	t.Helper()

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		AllowWildcardAdmission: new(allowWildcard),
		ProjectScoped:          nil,
	})
	require.NoError(t, err)

	return policy
}

func TestRegisterIssuer_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadIssuerCreate)
	require.NoError(t, err)

	policy := registerAnthropic(t, ctx, ti, true)

	require.Len(t, policy.Issuers, 1)
	issuer := policy.Issuers[0]
	require.Equal(t, "Claude Tag", issuer.Name)
	require.Equal(t, anthropicIssuer, issuer.Issuer)
	require.Equal(t, anthropicJWKS, issuer.JwksURI)
	require.True(t, issuer.AllowWildcardAdmission)
	// Organization tier by default: a federated platform's issuer is trusted by
	// the whole organization, and a caller that has not asked for a project-tier
	// row should not silently get one.
	require.Empty(t, issuer.ProjectID)
	// Every write returns the whole policy, so the empty admitted set is part of
	// the response rather than something the client has to fetch.
	require.Empty(t, policy.Admissions)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestRegisterIssuer_DefaultsToRefusingWildcards(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		AllowWildcardAdmission: nil,
		ProjectScoped:          nil,
	})
	require.NoError(t, err)

	// Default-off, and off is the resting state: a wildcard rule is a real
	// widening, so it has to be asked for at the issuer as well as per rule.
	require.False(t, policy.Issuers[0].AllowWildcardAdmission)
}

func TestRegisterIssuer_RefusesUnsafeURLs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	for _, tc := range []struct {
		name    string
		issuer  string
		jwksURI string
	}{
		// SEP-1933 MUST. jwks_uri is the only field the verification path reads,
		// so an http spelling puts key retrieval in the clear.
		{name: "http issuer", issuer: "http://identity.anthropic.com", jwksURI: anthropicJWKS},
		{name: "http jwks_uri", issuer: anthropicIssuer, jwksURI: "http://identity.anthropic.com/jwks"},

		// RFC 8414 §2: an issuer identifier carries no query or fragment.
		{name: "issuer with query", issuer: "https://identity.anthropic.com?x=1", jwksURI: anthropicJWKS},
		{name: "issuer with fragment", issuer: "https://identity.anthropic.com#f", jwksURI: anthropicJWKS},

		// WIMSE: a trust domain is a fully qualified domain name. Neither of
		// these names something whose ownership can be established.
		{name: "ip address issuer", issuer: "https://10.0.0.1", jwksURI: anthropicJWKS},
		{name: "single label issuer", issuer: "https://localhost", jwksURI: anthropicJWKS},
		{name: "single label jwks_uri", issuer: anthropicIssuer, jwksURI: "https://internal/jwks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
				SessionToken:           nil,
				ApikeyToken:            nil,
				ProjectSlugInput:       nil,
				Name:                   "Claude Tag " + tc.name,
				Issuer:                 tc.issuer,
				JwksURI:                tc.jwksURI,
				AllowWildcardAdmission: nil,
				ProjectScoped:          nil,
			})
			requireOopsCode(t, err, oops.CodeInvalid)
		})
	}
}

func TestRegisterIssuer_RefusesADuplicateNameAtTheSameTier(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	registerAnthropic(t, ctx, ti, false)

	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 "https://identity.example.com",
		JwksURI:                "https://identity.example.com/jwks",
		AllowWildcardAdmission: nil,
		ProjectScoped:          nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestRegisterIssuer_RequiresWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Reading the policy discloses which machines the organization recognises,
	// so it needs its own grant; it does not imply the ability to change it.
	readOnly := withScopes(t, ctx, ti, authz.ScopeWorkloadRead)

	_, err := ti.service.RegisterIssuer(readOnly, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		AllowWildcardAdmission: nil,
		ProjectScoped:          nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.List(readOnly, &gen.ListPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}

func TestRegisterIssuer_RefusesABlankName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   strings.Repeat(" ", 4),
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		AllowWildcardAdmission: nil,
		ProjectScoped:          nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
