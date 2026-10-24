package agentmanagement

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestReadableAgentIDsNamedReturnsOnlyReadableMatches(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	seedOrganizationUser(t, conn, "org-a", "other")
	owned := createAgent(t, conn, "org-a", "caller", "Deploy bot")
	granted := createAgent(t, conn, "org-a", "other", "Twin bot A")
	createAgent(t, conn, "org-a", "other", "Twin bot B")
	literal := createAgent(t, conn, "org-a", "caller", "50%_off bot")
	createAgent(t, conn, "org-a", "caller", "50xyoff bot")
	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "caller")

	ids, err := authorizer.ReadableAgentIDsNamed(ctx, conn, "deploy")
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{owned.ID}, ids)

	ids, err = authorizer.ReadableAgentIDsNamed(ctx, conn, "twin")
	require.NoError(t, err)
	require.Empty(t, ids, "another member's agents are hidden without agent:read")

	seedGrant(t, ctx, conn, "org-a", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentRead, granted.ID.String())
	ids, err = authorizer.ReadableAgentIDsNamed(ctx, conn, "twin")
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{granted.ID}, ids, "a read grant reveals only the agent it names")

	ids, err = authorizer.ReadableAgentIDsNamed(ctx, conn, "50%_off")
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{literal.ID}, ids)
}

func TestReadableAgentIDsNamedScansPastOneBatch(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	want := make([]uuid.UUID, 0, readableAgentBatchSize+5)
	for i := range readableAgentBatchSize + 5 {
		want = append(want, createAgent(t, conn, "org-a", "caller", fmt.Sprintf("Fleet %03d", i)).ID)
	}
	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})

	ids, err := authorizer.ReadableAgentIDsNamed(validatedHumanContext(t, "org-a", "caller"), conn, "fleet")
	require.NoError(t, err)
	require.ElementsMatch(t, want, ids)
}

func TestReadableAgentIDsNamedReadsNoneForCredentialsThatCannotManageAgents(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-a")
	seedOrganizationUser(t, conn, "org-a", "caller")
	createAgent(t, conn, "org-a", "caller", "Deploy bot")
	authorizer := NewAuthorizer(&fakeAuthorizationEngine{allowed: map[string]bool{}})

	human := validatedHumanContext(t, "org-a", "caller")
	apiKey := *mustAuthContext(t, human)
	apiKey.APIKeyID = uuid.NewString()
	for name, ctx := range map[string]context.Context{
		"api key":         contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &apiKey), &apiKey, false),
		"inactive member": validatedHumanContext(t, "org-a", "former"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ids, err := authorizer.ReadableAgentIDsNamed(ctx, conn, "deploy")
			require.NoError(t, err)
			require.Empty(t, ids)
		})
	}
}
