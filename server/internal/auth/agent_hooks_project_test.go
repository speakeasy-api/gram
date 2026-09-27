package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var hooksKeyScheme = &security.APIKeyScheme{
	Name:           constants.KeySecurityScheme,
	Scopes:         []string{},
	RequiredScopes: []string{"hooks"},
}

// createHooksAgentKey issues an org-wide agent key whose agent may read readable.
func createHooksAgentKey(t *testing.T, ctx context.Context, instance *testInstance, readable projectsrepo.Project) string {
	t.Helper()
	userInfo := defaultMockUserInfo()
	organizationID := userInfo.Organizations[0].ID

	agent, err := agentsrepo.New(instance.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: userInfo.UserID, Name: "Hooks project agent",
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	seedUserProjectGrant(t, ctx, instance, organizationID, userInfo.UserID, readable.ID.String())
	seedPrincipalProjectGrant(t, ctx, instance, organizationID, actor, readable.ID.String())

	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, readable.ID.String())})
	require.NoError(t, err)
	rawPolicy, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, policy)
	require.NoError(t, err)

	key := "gram_local_" + uuid.NewString()
	keyHash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)
	_, err = keysrepo.New(instance.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         organizationID,
		CreatedByUserID:        userInfo.UserID,
		Name:                   "hooks-agent-key",
		KeyPrefix:              key[:16],
		KeyHash:                keyHash,
		SubjectUrn:             pgtype.Text{String: actor.String(), Valid: true},
		DelegatedGrants:        rawPolicy,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.DelegatedPolicyVersion2), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return key
}

func TestAuthorizeAgentHooksKeySelectsReadableProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "hooks-readable", "hooks-other")
	key := createHooksAgentKey(t, ctx, instance, projects[0])

	admitted, err := instance.authorizer.Authorize(ctx, key, hooksKeyScheme)
	require.NoError(t, err, "unbound agent keys enter hooks routes")
	scoped, err := instance.authorizer.Authorize(admitted, projects[0].Slug, projectSlugScheme)
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(scoped)
	require.True(t, ok)
	require.Equal(t, projects[0].ID, *authCtx.ProjectID)
}

func TestAuthorizeAgentHooksKeyRejectsUnreadableProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, instance, projects := newProjectAccessTest(t, "hooks-readable", "hooks-other")
	key := createHooksAgentKey(t, ctx, instance, projects[0])

	admitted, err := instance.authorizer.Authorize(ctx, key, hooksKeyScheme)
	require.NoError(t, err)
	_, err = instance.authorizer.Authorize(admitted, projects[1].Slug, projectSlugScheme)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code, "project:read is enforced on the selected project")
}
