package workloadpolicy_test

import (
	"context"
	"fmt"
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
		Description:            nil,
		AllowWildcardAdmission: new(allowWildcard),
		Tags:                   nil,
		ProjectScoped:          false,
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

func TestRegisterIssuer_DefaultsToAllowingWildcards(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            nil,
		AllowWildcardAdmission: nil,
		Tags:                   nil,
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	require.True(t, policy.Issuers[0].AllowWildcardAdmission)
}

func TestRegisterIssuer_KeepsAnExplicitWildcardOptOut(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy := registerAnthropic(t, ctx, ti, false)

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
				Description:            nil,
				AllowWildcardAdmission: new(false),
				ProjectScoped:          false,
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
		Description:            nil,
		AllowWildcardAdmission: new(false),
		Tags:                   nil,
		ProjectScoped:          false,
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
		Description:            nil,
		AllowWildcardAdmission: new(false),
		Tags:                   nil,
		ProjectScoped:          false,
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
		Description:            nil,
		AllowWildcardAdmission: new(false),
		Tags:                   nil,
		ProjectScoped:          false,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRegisterIssuer_StoresTagsTrimmedAndDeduplicated(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   []string{"  production  ", "ci", "production"},
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	// Trimmed, de-duplicated, and in the order they were entered: a tag list is
	// something an operator reads back, so it should not be reordered under them.
	require.Equal(t, []string{"production", "ci"}, policy.Issuers[0].Tags)
}

func TestRegisterIssuer_RendersNoTagsAsAnEmptyList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy := registerAnthropic(t, ctx, ti, true)

	// Empty rather than null, so a client never has to distinguish "no tags"
	// from "field absent".
	require.NotNil(t, policy.Issuers[0].Tags)
	require.Empty(t, policy.Issuers[0].Tags)
}

func TestRegisterIssuer_RefusesABlankTag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   []string{"production", "   "},
		ProjectScoped:          false,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRegisterIssuer_RefusesAnOverlongTag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   []string{strings.Repeat("a", 65)},
		ProjectScoped:          false,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRegisterIssuer_StoresTheDescriptionTrimmed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            new("  Claude agents in our Slack workspace  "),
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	require.Equal(t, "Claude agents in our Slack workspace", policy.Issuers[0].Description)
}

func TestRegisterIssuer_RendersNoDescriptionAsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            new("   "),
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          false,
	})
	require.NoError(t, err)

	// A whitespace-only description is stored as none, so a card never renders
	// a blank line where the issuer URL would otherwise have been.
	require.Empty(t, policy.Issuers[0].Description)
}

func registerWithTags(ctx context.Context, ti *testInstance, tags []string) (*gen.WorkloadIdentityPolicy, error) {
	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            nil,
		AllowWildcardAdmission: new(true),
		Tags:                   tags,
		ProjectScoped:          false,
	})
	if err != nil {
		return nil, fmt.Errorf("register issuer: %w", err)
	}
	return policy, nil
}

func TestRegisterIssuer_AcceptsATagAtTheLengthLimit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// Runes, not bytes: 64 multi-byte characters are within the limit.
	tag := strings.Repeat("é", 64)
	policy, err := registerWithTags(ctx, ti, []string{tag})
	require.NoError(t, err)
	require.Equal(t, []string{tag}, policy.Issuers[0].Tags)
}

func TestRegisterIssuer_RefusesMoreThanFortyTags(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	tags := make([]string, 41)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag-%d", i)
	}
	_, err := registerWithTags(ctx, ti, tags)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRegisterIssuer_LimitsTagsAfterNormalizing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// 41 entries that de-duplicate to 40, one of them padded past 64
	// characters before trimming.
	tags := make([]string, 0, 41)
	for i := range 40 {
		tags = append(tags, fmt.Sprintf("tag-%d", i))
	}
	tags = append(tags, "tag-0")
	tags[1] = "  " + strings.Repeat("a", 64) + "  "

	policy, err := registerWithTags(ctx, ti, tags)
	require.NoError(t, err)
	require.Len(t, policy.Issuers[0].Tags, 40)
}

func TestRegisterIssuer_RefusesATagWithANulCharacter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := registerWithTags(ctx, ti, []string{"prod\x00uction"})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func registerWithDescription(ctx context.Context, ti *testInstance, description string) (*gen.WorkloadIdentityPolicy, error) {
	policy, err := ti.service.RegisterIssuer(ctx, &gen.RegisterIssuerPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "Claude Tag",
		Issuer:                 anthropicIssuer,
		JwksURI:                anthropicJWKS,
		Description:            &description,
		AllowWildcardAdmission: new(true),
		Tags:                   nil,
		ProjectScoped:          false,
	})
	if err != nil {
		return nil, fmt.Errorf("register issuer: %w", err)
	}
	return policy, nil
}

func TestRegisterIssuer_LimitsTheDescriptionAfterTrimming(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	// 500 multi-byte characters, padded: within the limit once trimmed.
	description := strings.Repeat("é", 500)
	policy, err := registerWithDescription(ctx, ti, "  "+description+"  ")
	require.NoError(t, err)
	require.Equal(t, description, policy.Issuers[0].Description)
}

func TestRegisterIssuer_RefusesAnOverlongDescription(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := registerWithDescription(ctx, ti, strings.Repeat("a", 501))
	requireOopsCode(t, err, oops.CodeInvalid)
}
