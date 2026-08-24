package jsonwebkeysets_test

import (
	"context"
	"encoding/json"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	extkeysgen "github.com/speakeasy-api/gram/server/gen/external_keys"
	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

func TestCreateSet_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	setCreatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetCreate)
	require.NoError(t, err)
	publishesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyPublish)
	require.NoError(t, err)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	require.Equal(t, "primary", set.Name)
	require.Equal(t, ek.ID, set.ExternalKeyID)
	require.Equal(t, ti.orgID, set.OrganizationID)

	// The first key mints straight to active and carries the thumbprint kid
	// inside its own published document.
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)
	key := keys[0]
	require.Equal(t, "active", key.KeyState)
	require.NotNil(t, key.ActivatedAt)
	require.NotEmpty(t, key.Kid)
	require.Equal(t, ek.ID, key.ExternalKeyID)

	doc, err := json.Marshal(key.PublicJwk)
	require.NoError(t, err)
	var jwk map[string]any
	require.NoError(t, json.Unmarshal(doc, &jwk))
	require.Equal(t, key.Kid, jwk["kid"])
	require.Equal(t, "ES256", jwk["alg"])
	require.Equal(t, "sig", jwk["use"])

	setCreatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetCreate)
	require.NoError(t, err)
	require.Equal(t, setCreatesBefore+1, setCreatesAfter)

	publishesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyPublish)
	require.NoError(t, err)
	require.Equal(t, publishesBefore+1, publishesAfter)

	// The mint hands its KMS client back; a leak here has no other symptom.
	require.Equal(t, 1, ti.kms.Closed())
}

func TestCreateSet_NameRequired(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "   ",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateSet_InvalidExternalKeyID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: "not-a-uuid",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateSet_ExternalKeyNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateSet_AwsKeyRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	aws := createAwsKmsKey(t, ctx, ti, "aws-key")

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: aws.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateSet_SoftDeletedExternalKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")

	require.NoError(t, ti.ekService.DeleteGcpKmsKey(adminCtx(t, ctx), &extkeysgen.DeleteGcpKmsKeyPayload{
		ID:           ek.ID,
		SessionToken: nil,
	}))

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCreateSet_AlgorithmMismatch scripts a key whose real algorithm disagrees
// with the one recorded against it: the fixture records ES256, the scripted KMS
// reports RS256. The mint must refuse rather than publish a JWK advertising
// signatures no verifier would accept.
func TestCreateSet_AlgorithmMismatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")

	ti.kms.SetBuild(func(_ context.Context, _ oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return gcpkms.NewLocalSigningClient(jose.RS256)
	})

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// The refused mint still closes the client it opened.
	require.Equal(t, 1, ti.kms.Closed())
}

// TestCreateSet_CredentialDeleted reproduces a backing key orphaned by a
// credential soft-deleted out from under it: the mint refuses with a message
// naming the credential rather than pretending the key is missing.
func TestCreateSet_CredentialDeleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "doomed-cred")
	ek := createGcpKmsKey(t, ctx, ti, "signing-key", credID)
	softDeleteCredentialDirect(t, ctx, ti, credID)

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
