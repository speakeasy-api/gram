package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateAwsIamCredential_SwitchToWebIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-switch")
	require.NotNil(t, created.ExternalID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            created.ID,
		SessionToken:  nil,
		Name:          "aws-switch-renamed",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  new("sts.amazonaws.com"),
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)

	require.Equal(t, "aws-switch-renamed", updated.Name)
	require.Equal(t, "sts.amazonaws.com", *updated.OidcAudience)
	require.Nil(t, updated.ExternalID, "adding oidc_audience must clear the external_id")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAwsIamCredentialUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestUpdateAwsIamCredential_PreservesExternalID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-preserve")
	require.NotNil(t, created.ExternalID)
	originalExternalID := *created.ExternalID

	updated, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            created.ID,
		SessionToken:  nil,
		Name:          "aws-preserve-renamed",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram-v2"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)

	require.NotNil(t, updated.ExternalID)
	require.Equal(t, originalExternalID, *updated.ExternalID, "re-saving in external_id mode must preserve the existing external_id")
	require.Equal(t, "arn:aws:iam::123456789012:role/gram-v2", *updated.AssumeRoleArn)
}

func TestUpdateAwsIamCredential_SwitchToKeyPolicyGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-to-key-policy")
	require.NotNil(t, created.ExternalID)

	updated, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            created.ID,
		SessionToken:  nil,
		Name:          "aws-to-key-policy",
		AssumeRoleArn: nil,
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)

	require.Nil(t, updated.AssumeRoleArn, "clearing assume_role_arn moves the credential to a key-policy grant")
	require.Nil(t, updated.ExternalID, "the external_id must be cleared when no role is assumed")
}

func TestUpdateAwsIamCredential_AuditSnapshotRedactsExternalID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-redact")
	require.NotNil(t, created.ExternalID)
	rawExternalID := *created.ExternalID

	_, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            created.ID,
		SessionToken:  nil,
		Name:          "aws-redact-renamed",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	require.NoError(t, err)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAwsIamCredentialUpdate)
	require.NoError(t, err)

	// The snapshots record only whether an external_id is configured, never the value.
	require.Contains(t, string(rec.BeforeSnapshot), "has_external_id")
	require.Contains(t, string(rec.AfterSnapshot), "has_external_id")
	require.NotContains(t, string(rec.BeforeSnapshot), rawExternalID, "raw external_id must never appear in an audit snapshot")
	require.NotContains(t, string(rec.AfterSnapshot), rawExternalID, "raw external_id must never appear in an audit snapshot")
	require.NotContains(t, string(rec.Metadata), rawExternalID)
}

func TestUpdateAwsIamCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            uuid.NewString(),
		SessionToken:  nil,
		Name:          "missing",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateAwsIamCredential_WrongProviderNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-for-aws-update")

	_, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            gcp.ID,
		SessionToken:  nil,
		Name:          "wrong-provider",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateAwsIamCredential_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            "not-a-uuid",
		SessionToken:  nil,
		Name:          "bad-id",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateAwsIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-update-forbidden")

	_, err := ti.service.UpdateAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.UpdateAwsIamCredentialPayload{
		ID:            created.ID,
		SessionToken:  nil,
		Name:          "aws-update-forbidden",
		AssumeRoleArn: new("arn:aws:iam::123456789012:role/gram"),
		OidcAudience:  nil,
		OidcSubject:   nil,
		StsRegion:     nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
