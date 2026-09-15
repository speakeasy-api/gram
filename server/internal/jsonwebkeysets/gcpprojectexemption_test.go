package jsonwebkeysets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Minting signs with the credential's identity, so a key backed by a service
// account in Gram's own project would sign as an internal identity on behalf of
// a customer. Creating a set mints its first key, so the screening refuses the
// set outright.
func TestCreateSet_RefusesTargetInGramProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredentialDirect(t, ctx, ti, "legacy-unscreened", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText(gramProjectServiceAccount("internal")),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   false,
	})
	ek := createGcpKmsKey(t, ctx, ti, "behind-internal-sa", credID)

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "refused",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A platform administrator can grant that credential an exemption. Minting runs
// with no request actor, so it has to read the grant from the row: otherwise the
// credential would save and then fail on every signature it was created for.
// Both mint sites are covered here, since creating the set mints once and
// publishing into it mints again.
func TestPublishKey_HonorsStoredProjectVerificationExemption(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredentialDirect(t, ctx, ti, "staff-exempted", extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      uuid.Nil,
		ImpersonateServiceAccount: conv.ToPGText(gramProjectServiceAccount("internal")),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   true,
	})
	ek := createGcpKmsKey(t, ctx, ti, "behind-exempted-sa", credID)
	set := createSet(t, ctx, ti, "exempted", ek.ID)

	published, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, ek.ID, published.ExternalKeyID)
}
