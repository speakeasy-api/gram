package mcpconnectionhealth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
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

// check runs one read and reports whether the flag let it through.
func (ti *testInstance) check(t *testing.T, ctx context.Context) bool {
	t.Helper()
	_, err := ti.service.GetSessionActivity(ctx, &gen.GetSessionActivityPayload{UserSessionIssuerID: ti.issuer.String(), From: ti.rangeFrom(), To: ti.rangeTo()})
	if err == nil {
		return true
	}
	requireOopsCode(t, err, oops.CodeNotFound)
	return false
}

// requireChecked asserts the local check's identity and whether the
// organization check ran after it.
func (ti *testInstance) requireChecked(t *testing.T, distinctID string, email string, organizationCheck bool) {
	t.Helper()
	local, evaluated := ti.flags.calls()
	require.Equal(t, []flagCall{{distinctID: distinctID, personProperties: map[string]string{"email": email}}}, local)
	if organizationCheck {
		require.Equal(t, []flagCall{{distinctID: ti.org, personProperties: nil}}, evaluated)
	} else {
		require.Empty(t, evaluated)
	}
}

func TestFlag_OnForAPersonByEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetPeople("on@example.com")

	ti.readsWork(t, asPerson(t, ctx, new("on@example.com")))
	ti.flags.calls()
	require.True(t, ti.check(t, asPerson(t, ctx, new("on@example.com"))))
	ti.requireChecked(t, "on@example.com", "on@example.com", false)

	ti.readsAreNotFound(t, asPerson(t, ctx, new("off@example.com")))
	ti.flags.calls()
	require.False(t, ti.check(t, asPerson(t, ctx, new("off@example.com"))))
	ti.requireChecked(t, "off@example.com", "off@example.com", true)
}

func TestFlag_MatchesAnEmailWhateverItsCaseOrSpacing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.flags.targetPeople("on@example.com")

	require.True(t, ti.check(t, asPerson(t, ctx, new("On@Example.COM"))))
	ti.requireChecked(t, "On@Example.COM", "on@example.com", false)
	require.True(t, ti.check(t, asPerson(t, ctx, new("  on@example.com\n"))))
	ti.requireChecked(t, "  on@example.com\n", "on@example.com", false)
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

func TestFlag_WithoutLocalEvaluationTheOrganizationCheckDecidesAsBefore(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.flags.withoutLocalEvaluation()

	require.True(t, ti.check(t, ctx))
	ti.requireChecked(t, *mustEmail(t, ctx), *mustEmail(t, ctx), true)
	require.True(t, ti.check(t, asProducerKey(t, ctx)))
	ti.requireChecked(t, ti.org, "", true)

	ti.flags.targetPeople(*mustEmail(t, ctx))
	require.False(t, ti.check(t, ctx), "people cannot be matched without local evaluation")
}

func TestFlag_AnAPIKeyNeverMatchesAPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.targetPeople(*mustEmail(t, ctx))

	ti.readsWork(t, ctx)
	ti.readsAreNotFound(t, asProducerKey(t, ctx))
	ti.flags.calls()
	require.False(t, ti.check(t, asProducerKey(t, ctx)))
	ti.requireChecked(t, ti.org, "", true)
}

func TestFlag_ASessionWithoutEmailIsEvaluatedAsTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.flags.targetPeople("on@example.com")

	require.False(t, ti.check(t, asPerson(t, ctx, nil)))
	ti.requireChecked(t, ti.org, "", true)
	require.False(t, ti.check(t, asPerson(t, ctx, new("  "))))
	ti.requireChecked(t, ti.org, "", true)
}

func TestFlag_AnEvaluationErrorIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withProjectGrants(t, ctx, authz.ScopeProjectRead, authz.ScopeLogRead)
	ti.flags.fail(errors.New("flag provider unavailable"))

	ti.readsAreNotFound(t, ctx)
	ti.readsAreNotFound(t, asProducerKey(t, ctx))
}

// The checked-in flags.csv keyed the flag to the local organization; the
// in-memory provider must still open it for sessions and keys either way.
func TestFlag_TheLocalInMemoryProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	for name, key := range map[string]string{"organization row": ti.org, "every caller": feature.AnyDistinctID} {
		flags := &feature.InMemory{}
		flags.SetFlag(feature.FlagMCPConnectionHealth, key, true)
		ti.flags.use(flags)
		require.True(t, ti.check(t, ctx), name)
		require.True(t, ti.check(t, asProducerKey(t, ctx)), name)
	}
}

func mustEmail(t *testing.T, ctx context.Context) *string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	return authCtx.Email
}
