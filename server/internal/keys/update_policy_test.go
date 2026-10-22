package keys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestKeysService_UpdateKeyPolicy(t *testing.T) {
	t.Parallel()

	t.Run("replaces the policy and audits both sides", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		client := seedAuthClient(t, ctx, ti)

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "update" + randstr(6), Scopes: []string{"consumer"},
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)}})
		require.NoError(t, err)

		before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyPolicyUpdate)
		require.NoError(t, err)

		updated, err := ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{
			keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project),
			keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCPAuthClient, client, nil),
		}})
		require.NoError(t, err)
		require.Len(t, updated.PolicyGrants, 2)
		require.Nil(t, updated.Key, "the secret is never returned again")

		grants, err := authz.LoadGrants(ctx, ti.conn, key.OrganizationID, []urn.Principal{urn.NewAPIKeyPrincipal(uuid.MustParse(key.ID))})
		require.NoError(t, err)
		require.Len(t, grants, 2)

		after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyPolicyUpdate)
		require.NoError(t, err)
		require.Equal(t, before+1, after)

		cleared, err := ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{}})
		require.NoError(t, err)
		require.Empty(t, cleared.PolicyGrants)
		grants, err = authz.LoadGrants(ctx, ti.conn, key.OrganizationID, []urn.Principal{urn.NewAPIKeyPrincipal(uuid.MustParse(key.ID))})
		require.NoError(t, err)
		require.Empty(t, grants)
	})

	t.Run("refuses an auth client from another organization", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "foreign" + randstr(6), Scopes: []string{"consumer"}})
		require.NoError(t, err)

		_, err = ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{
			keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCPAuthClient, uuid.NewString(), nil),
		}})
		requireCode(t, err, oops.CodeBadRequest)
	})

	t.Run("needs org admin", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "admin" + randstr(6), Scopes: []string{"consumer"}})
		require.NoError(t, err)

		ctx = authztest.WithExactGrants(t, ctx)
		_, err = ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{}})
		requireCode(t, err, oops.CodeForbidden)
	})

	t.Run("a revoked key is not found", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "revoked" + randstr(6), Scopes: []string{"consumer"}})
		require.NoError(t, err)
		require.NoError(t, ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: key.ID}))

		_, err = ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{}})
		requireCode(t, err, oops.CodeNotFound)
	})
}
