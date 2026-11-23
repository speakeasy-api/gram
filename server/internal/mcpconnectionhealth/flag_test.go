package mcpconnectionhealth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (ti *testInstance) readsWork(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := ti.service.ListAttempts(ctx, ti.listPayload())
	require.NoError(t, err)
	_, err = ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	require.NoError(t, err)
}

func (ti *testInstance) readsAreNotFound(t *testing.T, ctx context.Context) {
	t.Helper()
	_, err := ti.service.ListAttempts(ctx, ti.listPayload())
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// asPerson keeps the signed-in session and swaps its email; nil drops it.
func asPerson(t *testing.T, ctx context.Context, email *string) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	person := *authCtx
	person.Email = email
	return contextvalues.SetAuthContext(ctx, &person)
}

// asProducerKey authenticates as an API key created by the signed-in person;
// the key row carries its creator's email.
func asProducerKey(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return contextvalues.WithLegacyAPIKeyAuthorization(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		OrganizationSlug:     authCtx.OrganizationSlug,
		ProjectID:            authCtx.ProjectID,
		ProjectSlug:          authCtx.ProjectSlug,
		UserID:               authCtx.UserID,
		Email:                authCtx.Email,
		APIKeyID:             "key-id",
		APIKeyName:           "producer",
		APIKeyScopes:         []string{"producer"},
	})
}

func (ti *testInstance) requireEvaluatedAs(t *testing.T, distinctID string, personProperties map[string]string) {
	t.Helper()
	gotID, gotProperties := ti.flags.lastCall()
	require.Equal(t, distinctID, gotID)
	require.Equal(t, personProperties, gotProperties)
}

func TestFlag_OnForAPersonByEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetPeople("on@example.com")

	ti.readsWork(t, asPerson(t, ctx, new("on@example.com")))
	ti.requireEvaluatedAs(t, "on@example.com", map[string]string{"email": "on@example.com"})
	ti.readsAreNotFound(t, asPerson(t, ctx, new("off@example.com")))
	ti.requireEvaluatedAs(t, "off@example.com", map[string]string{"email": "off@example.com"})
}

func TestFlag_MatchesAnEmailWhateverItsCaseOrSpacing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetPeople("on@example.com")

	ti.readsWork(t, asPerson(t, ctx, new("On@Example.COM")))
	ti.requireEvaluatedAs(t, "On@Example.COM", map[string]string{"email": "on@example.com"})
	ti.readsWork(t, asPerson(t, ctx, new("  on@example.com\n")))
	ti.requireEvaluatedAs(t, "  on@example.com\n", map[string]string{"email": "on@example.com"})
}

func TestFlag_OnForTheOrganizationWorksForEveryone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)

	ti.readsWork(t, asPerson(t, ctx, new("on@example.com")))
	ti.readsWork(t, asPerson(t, ctx, new("off@example.com")))
	ti.readsWork(t, asPerson(t, ctx, nil))
	ti.readsWork(t, asProducerKey(t, ctx))
}

func TestFlag_AnAPIKeyNeverMatchesAPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	ti.flags.targetPeople(*authCtx.Email)

	ti.readsWork(t, ctx)
	ti.readsAreNotFound(t, asProducerKey(t, ctx))
	ti.requireEvaluatedAs(t, ti.org, map[string]string{"email": ""})
}

func TestFlag_ASessionWithoutEmailIsEvaluatedAsTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetPeople("on@example.com")

	ti.readsAreNotFound(t, asPerson(t, ctx, nil))
	ti.requireEvaluatedAs(t, ti.org, map[string]string{"email": ""})
	ti.readsAreNotFound(t, asPerson(t, ctx, new("  ")))
	ti.requireEvaluatedAs(t, ti.org, map[string]string{"email": ""})
}

func TestFlag_AnEvaluationErrorIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.fail(errors.New("flag provider unavailable"))

	ti.readsAreNotFound(t, ctx)
}
