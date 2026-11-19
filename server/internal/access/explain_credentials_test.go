package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

func seedServerAuthClient(t *testing.T, ctx context.Context, ti *testInstance, orgID, serverID, clientType, name string) string {
	t.Helper()

	repo := authclientsrepo.New(ti.conn)
	client, err := repo.CreateAuthClient(ctx, authclientsrepo.CreateAuthClientParams{
		OrganizationID:        orgID,
		ProjectID:             uuid.NullUUID{},
		ClientType:            clientType,
		Name:                  name,
		CreatedByUserID:       pgtype.Text{},
		RemoteSessionClientID: uuid.NullUUID{},
		ClientAttachmentScope: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = repo.AttachAuthClientToServer(ctx, authclientsrepo.AttachAuthClientToServerParams{
		McpServerID:    uuid.MustParse(serverID),
		AuthClientID:   client.ID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	return client.ID.String()
}

func explainedCredential(t *testing.T, result *gen.ExplainResourceAccessResult, id string) *gen.ExplainedCredentialAccess {
	t.Helper()

	for _, credential := range result.Credentials {
		if credential.AuthClientID == id {
			return credential
		}
	}
	require.FailNow(t, "credential not explained", id)
	return nil
}

func TestService_ExplainResourceAccess_ExplainsEachCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)
	bot := seedServerAuthClient(t, ctx, ti, fixture.orgID, fixture.serverID, authz.AuthClientTypeServiceAccount, "Reporting bot")
	shared := seedServerAuthClient(t, ctx, ti, fixture.orgID, fixture.serverID, authz.AuthClientTypeStaticCredential, "Shared key")

	engineer := seededRolePrincipal(t, ctx, ti.conn, fixture.orgID, "engineer")
	seedGrantWithSelector(t, ctx, ti.conn, fixture.orgID, engineer, authz.ScopeMCPAuth, authz.AuthClientSelector(bot, ""))
	seedGrantWithSelector(t, ctx, ti.conn, fixture.orgID, engineer, authz.ScopeMCPAuth, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient,
		authz.SelectorKeyResourceID:   authz.WildcardResource,
		authz.SelectorKeyClientType:   authz.AuthClientTypeStaticCredential,
	})
	contractors := seededRolePrincipal(t, ctx, ti.conn, fixture.orgID, "contractors")
	seedGrantWithSelector(t, ctx, ti.conn, fixture.orgID, contractors, authz.ScopeMCPBlockedAuth, authz.AuthClientSelector(shared, ""))

	result := explainAccess(t, ctx, ti, fixture.serverID, fixture.userID)
	require.Len(t, result.Credentials, 2)

	allowed := explainedCredential(t, result, bot)
	require.True(t, allowed.Allowed)
	require.True(t, allowed.Enabled)
	require.Equal(t, authz.AuthClientTypeServiceAccount, allowed.ClientType)
	require.Len(t, allowed.Rules, 1)
	require.Equal(t, "Engineer", allowed.Rules[0].DisplayName)
	require.Equal(t, "credential", allowed.Rules[0].AppliesTo)
	require.Equal(t, string(authz.GrantEffectAllows), allowed.Rules[0].Effect)

	// The block from the mapped role wins over the kind-wide allow.
	blocked := explainedCredential(t, result, shared)
	require.False(t, blocked.Allowed)
	effects := map[string]string{}
	for _, rule := range blocked.Rules {
		effects[rule.DisplayName+"/"+rule.AppliesTo] = rule.Effect
	}
	require.Equal(t, map[string]string{
		"Contractors/credential":   string(authz.GrantEffectBlocks),
		"Engineer/credential_type": string(authz.GrantEffectBlocked),
	}, effects)
	require.True(t, blocked.Rules[0].ViaDirectoryMapping)
}

func TestService_ExplainResourceAccess_NoCredentials(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)

	result := explainAccess(t, ctx, ti, fixture.serverID, fixture.userID)
	require.Empty(t, result.Credentials)
}
