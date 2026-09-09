package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/stretchr/testify/require"
)

func seedAgentAPIKey(t *testing.T, conn *pgxpool.Pool, org, creator string, subject *string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := conn.Exec(t.Context(), `INSERT INTO api_keys (id, organization_id, created_by_user_id, name, key_prefix, key_hash, subject_urn) VALUES ($1, $2, $3, 'Test key ' || $4::text, 'test', $4, $5)`, id, org, creator, id.String(), subject) //nolint:glint // notestingrawsql: constructs exact credential binding fixtures
	require.NoError(t, err)
	return id
}

func TestAgentAPIKeysExactBindingAndAuditedRevoke(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	const org = "org-agent-keys"
	seedOrganization(t, conn, org)
	seedOrganizationUser(t, conn, org, "owner")
	seedOrganizationUser(t, conn, org, "other")
	agent := createAgent(t, conn, org, "owner", "Agent")
	subject := "agent:" + agent.ID.String()
	otherSubject := "agent:" + uuid.NewString()
	// Creator attribution deliberately differs from the owner; binding is subject-only.
	key := seedAgentAPIKey(t, conn, org, "other", &subject)
	legacy := seedAgentAPIKey(t, conn, org, "owner", nil)
	other := seedAgentAPIKey(t, conn, org, "owner", &otherSubject)
	seedOrganization(t, conn, "org-agent-keys-other")
	foreign := seedAgentAPIKey(t, conn, "org-agent-keys-other", "owner", &subject)
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, org, "owner")
	payload := &gen.ListAPIKeysPayload{AgentID: agent.ID.String()}
	listed, err := service.ListAPIKeys(ctx, payload)
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, key.String(), listed.Items[0].ID)
	require.Nil(t, listed.Items[0].ExpiresAt)
	require.Nil(t, listed.NextCursor)

	for _, id := range []uuid.UUID{legacy, other, foreign, uuid.New()} {
		require.NoError(t, service.RevokeAPIKey(ctx, &gen.RevokeAPIKeyPayload{AgentID: agent.ID.String(), KeyID: id.String()}))
	}
	var deleted bool
	for _, id := range []uuid.UUID{legacy, other, foreign} {
		require.NoError(t, conn.QueryRow(t.Context(), `SELECT deleted FROM api_keys WHERE id = $1`, id).Scan(&deleted)) //nolint:glint // notestingrawsql: verifies credential isolation
		require.False(t, deleted)
	}
	revoke := &gen.RevokeAPIKeyPayload{AgentID: agent.ID.String(), KeyID: key.String()}
	require.NoError(t, service.RevokeAPIKey(ctx, revoke))
	require.NoError(t, service.RevokeAPIKey(ctx, revoke))
	listed, err = service.ListAPIKeys(ctx, payload)
	require.NoError(t, err)
	require.Empty(t, listed.Items)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND action = 'api_key:revoke' AND actor_id = 'owner'`, org).Scan(&count)) //nolint:glint // notestingrawsql: verifies exactly-once human audit attribution
	require.Equal(t, 1, count)
}

func TestAgentAPIKeysAuthorizationAndPagination(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	const org = "org-agent-key-pages"
	seedOrganization(t, conn, org)
	seedOrganizationUser(t, conn, org, "owner")
	seedOrganizationUser(t, conn, org, "other")
	agent := createAgent(t, conn, org, "owner", "Agent")
	subject := "agent:" + agent.ID.String()
	for range 101 {
		seedAgentAPIKey(t, conn, org, "other", &subject)
	}
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, org, "owner")
	first, err := service.ListAPIKeys(ctx, &gen.ListAPIKeysPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Len(t, first.Items, 100)
	require.NotNil(t, first.NextCursor)
	second, err := service.ListAPIKeys(ctx, &gen.ListAPIKeysPayload{AgentID: agent.ID.String(), Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.Nil(t, second.NextCursor)
	for _, item := range first.Items {
		require.NotEqual(t, second.Items[0].ID, item.ID)
	}
	unauthorized := validatedHumanContext(t, org, "other")
	_, err = service.ListAPIKeys(unauthorized, &gen.ListAPIKeysPayload{AgentID: agent.ID.String()})
	require.Error(t, err)
	require.Error(t, service.RevokeAPIKey(unauthorized, &gen.RevokeAPIKeyPayload{AgentID: agent.ID.String(), KeyID: first.Items[0].ID}))
	_, err = service.ListAPIKeys(t.Context(), &gen.ListAPIKeysPayload{AgentID: agent.ID.String()})
	require.Error(t, err)
	invalid := "invalid-cursor"
	_, err = service.ListAPIKeys(ctx, &gen.ListAPIKeysPayload{AgentID: agent.ID.String(), Cursor: &invalid})
	require.Error(t, err)
}

func TestAgentAPIKeyRevokeRollsBackWhenAuditFails(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	const org = "org-agent-key-rollback"
	seedOrganization(t, conn, org)
	seedOrganizationUser(t, conn, org, "owner")
	agent := createAgent(t, conn, org, "owner", "Agent")
	subject := "agent:" + agent.ID.String()
	key := seedAgentAPIKey(t, conn, org, "owner", &subject)
	_, err := conn.Exec(t.Context(), `CREATE FUNCTION reject_key_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_key_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_key_audit();`) //nolint:glint // notestingrawsql: injects transactional audit failure
	require.NoError(t, err)
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, org, "owner")
	require.Error(t, service.RevokeAPIKey(ctx, &gen.RevokeAPIKeyPayload{AgentID: agent.ID.String(), KeyID: key.String()}))
	var deleted bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT deleted FROM api_keys WHERE id = $1`, key).Scan(&deleted)) //nolint:glint // notestingrawsql: verifies atomic rollback
	require.False(t, deleted)
}
