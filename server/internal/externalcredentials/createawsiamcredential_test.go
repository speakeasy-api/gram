package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateAwsIamCredential_AssumeRoleWithExternalID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialCreate)
	require.NoError(t, err)

	cred, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-external-id",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     new("us-east-1"),
	})
	require.NoError(t, err)
	require.NotNil(t, cred)

	require.Equal(t, "aws_iam", cred.Provider)
	require.Equal(t, "arn:aws:iam::123456789012:role/gram", *cred.AssumeRoleArn)
	require.NotNil(t, cred.ExternalID)
	require.NotEmpty(t, *cred.ExternalID, "assume_role_arn without oidc_audience gets a Gram-generated external_id")
	require.Nil(t, cred.OidcAudience)
	require.Equal(t, "us-east-1", *cred.StsRegion)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreateAwsIamCredential_AssumeRoleWithWebIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-web-identity",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  new("sts.amazonaws.com"),
		OidcSubject:   new("system:serviceaccount:gram:signer"),
		StsRegion:     nil,
	})
	require.NoError(t, err)

	require.Equal(t, "sts.amazonaws.com", *cred.OidcAudience)
	require.Equal(t, "system:serviceaccount:gram:signer", *cred.OidcSubject)
	require.Nil(t, cred.ExternalID, "web-identity credentials must not carry an external_id")
}

func TestCreateAwsIamCredential_KeyPolicyGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-key-policy",
		AssumeRoleArn: nil,
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)

	require.Nil(t, cred.AssumeRoleArn)
	require.Nil(t, cred.ExternalID)
	require.Nil(t, cred.OidcAudience)
}

func TestCreateAwsIamCredential_MissingName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "   ",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsIamCredential_AudienceWithoutArn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-audience-no-arn",
		AssumeRoleArn: nil,
		OidcAudience:  new("sts.amazonaws.com"),
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsIamCredential_SubjectWithoutAudience(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-subject-no-audience",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   new("system:serviceaccount:gram:signer"),
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsIamCredential_StsRegionWithoutArn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-region-no-arn",
		AssumeRoleArn: nil,
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     new("us-east-1"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateAwsIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.CreateAwsIamCredentialPayload{
		SessionToken:  nil,
		Name:          "aws-forbidden",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
