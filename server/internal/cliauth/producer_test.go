package cliauth_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/cli_auth"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authztest"
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

	var (
		keyID     uuid.UUID
		projectID *uuid.UUID
		scopes    []string
		name      string
	)
	err = ti.conn.QueryRow(ctx, `SELECT id, project_id, scopes, name FROM api_keys WHERE key_hash = $1`, keyHash).Scan(&keyID, &projectID, &scopes, &name)
	require.NoError(t, err)
	require.Nil(t, projectID, "a CLI producer key is organization-wide")
	require.Equal(t, []string{auth.APIKeyScopeProducer.String()}, scopes)
	require.True(t, strings.HasPrefix(name, "CLI Key (Generated) - "))

	var audited int
	err = ti.conn.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = $1 AND subject_id = $2`, string(audit.ActionKeyCreate), keyID.String()).Scan(&audited)
	require.NoError(t, err)
	require.Equal(t, 1, audited)
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
