package cliauth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/cli_auth"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// An org admin's producer code redeems for an organization-wide producer key,
// stored and audited the way keys.create stores one for the dashboard.
func TestAuthorize_ProducerKeyForAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = authztest.WithAdminGrants(ctx)
	verifier, challenge := pkcePair(t)

	authorized, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		KeyScope:            "producer",
		SessionToken:        nil,
	})
	require.NoError(t, err)

	redeemed, err := ti.service.Redeem(ctx, &gen.RedeemPayload{
		Code:         authorized.Code,
		CodeVerifier: verifier,
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(redeemed.AccessToken, "gram_local_"))
	require.NotEmpty(t, redeemed.ProjectSlug)

	keyHash, err := auth.GetAPIKeyHash(redeemed.AccessToken)
	require.NoError(t, err)

	key, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.NoError(t, err)
	require.False(t, key.ProjectID.Valid, "a CLI producer key is organization-wide")
	require.Equal(t, []string{auth.APIKeyScopeProducer.String()}, key.Scopes)
	require.True(t, strings.HasPrefix(key.Name, "CLI Key (Generated) - "))

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionKeyCreate, key.ID.String())
	require.NoError(t, err)
	require.Equal(t, key.ID.String(), record.SubjectID)
	created, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	require.Equal(t, int64(1), created, "minting the key writes exactly one key.create audit row")
}

// Producer keys take org:admin, so a member without it cannot get a producer
// code through the CLI flow any more than through the dashboard.
func TestAuthorize_ProducerKeyRequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)
	_, challenge := pkcePair(t)

	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		KeyScope:            "producer",
		SessionToken:        nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAuthorize_UnknownKeyScopeRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, challenge := pkcePair(t)

	_, err := ti.service.Authorize(ctx, &gen.AuthorizePayload{
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ProjectSlug:         nil,
		KeyScope:            "consumer",
		SessionToken:        nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
