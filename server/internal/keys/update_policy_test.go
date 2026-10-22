package keys_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// auditedPolicy decodes one side of the latest policy update audit entry.
func auditedPolicy(t *testing.T, snapshot []byte) []map[string]any {
	t.Helper()
	var decoded struct {
		PolicyGrants []map[string]any `json:"policy_grants"`
	}
	require.NoError(t, json.Unmarshal(snapshot, &decoded))
	require.NotNil(t, decoded.PolicyGrants, "a whole policy is recorded, never null")
	return decoded.PolicyGrants
}

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
		record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionKeyPolicyUpdate)
		require.NoError(t, err)
		require.Len(t, auditedPolicy(t, record.BeforeSnapshot), 1)
		require.Len(t, auditedPolicy(t, record.AfterSnapshot), 2)

		cleared, err := ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{}})
		require.NoError(t, err)
		require.Empty(t, cleared.PolicyGrants)
		grants, err = authz.LoadGrants(ctx, ti.conn, key.OrganizationID, []urn.Principal{urn.NewAPIKeyPrincipal(uuid.MustParse(key.ID))})
		require.NoError(t, err)
		require.Empty(t, grants)
		record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionKeyPolicyUpdate)
		require.NoError(t, err)
		require.Empty(t, auditedPolicy(t, record.AfterSnapshot))
	})

	t.Run("an explicitly emptied policy stays empty through the backfill", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)

		fixtures := testrepo.New(ti.conn)
		remoteID := uuid.New()
		require.NoError(t, fixtures.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{
			ID: remoteID, ProjectID: uuid.MustParse(project), Url: "https://upstream.example.com/" + remoteID.String(),
		}))
		serverID := uuid.New()
		require.NoError(t, fixtures.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
			ID: serverID, ProjectID: uuid.MustParse(project), Slug: conv.ToPGText("srv-" + serverID.String()[:8]),
			RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: visibility.Private,
		}))

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "emptied" + randstr(6), Scopes: []string{"consumer"},
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)}})
		require.NoError(t, err)

		cleared, err := ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{}})
		require.NoError(t, err)
		organizationID := key.OrganizationID
		row, err := keysrepo.New(ti.conn).GetAPIKeyByIDForUpdate(ctx, keysrepo.GetAPIKeyByIDForUpdateParams{ID: uuid.MustParse(key.ID), OrganizationID: organizationID})
		require.NoError(t, err)
		require.True(t, row.UpdatedAt.Time.After(row.CreatedAt.Time), "the key's updated_at moves with its policy")
		require.Equal(t, row.UpdatedAt.Time.Format(time.RFC3339), cleared.UpdatedAt)

		_, err = authclientbackfill.RunGrants(ctx, ti.conn, authclientbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
		require.NoError(t, err)

		grants, err := authz.LoadGrants(ctx, ti.conn, organizationID, []urn.Principal{urn.NewAPIKeyPrincipal(uuid.MustParse(key.ID))})
		require.NoError(t, err)
		require.Empty(t, grants, "the backfill must not give an emptied key its legacy reach back")
	})

	t.Run("refuses an auth client from another organization", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "foreign" + randstr(6), Scopes: []string{"consumer"}})
		require.NoError(t, err)

		fixtures := testrepo.New(ti.conn)
		otherOrg := "org-" + uuid.NewString()
		require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
			ID: otherOrg, Name: otherOrg, Slug: otherOrg, GramAccountType: "free",
			WorkosID: pgtype.Text{String: "", Valid: false}, Whitelisted: false,
			FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
			DisabledAt: pgtype.Timestamptz{}, CreatedAt: pgtype.Timestamptz{},
		}))
		foreign, err := fixtures.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
			OrganizationID: otherOrg, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ClientType: authz.AuthClientTypeServiceAccount, Name: "foreign " + randstr(6),
		})
		require.NoError(t, err)

		_, err = ti.service.UpdateKeyPolicy(ctx, &gen.UpdateKeyPolicyPayload{ID: key.ID, PolicyGrants: []*gen.APIKeyPolicyGrant{
			keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCPAuthClient, foreign.String(), nil),
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
