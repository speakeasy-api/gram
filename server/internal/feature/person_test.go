package feature_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestFlagTarget(t *testing.T) {
	t.Parallel()

	auth := func(email *string) *contextvalues.AuthContext {
		return &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", Email: email}
	}
	session := func(email *string) context.Context {
		return contextvalues.WithValidatedGramSession(t.Context(), auth(email), false)
	}

	id, properties, person := feature.FlagTarget(session(new(" On@Example.com ")), "org")
	require.True(t, person)
	require.Equal(t, " On@Example.com ", id, "the dashboard identifies with the raw email")
	require.Equal(t, map[string]string{"email": "on@example.com"}, properties)

	for name, ctx := range map[string]context.Context{
		"no auth context":  t.Context(),
		"session no email": session(nil),
		"session blank":    session(new("  ")),
		"api key":          contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), auth(new("on@example.com"))),
	} {
		id, properties, person := feature.FlagTarget(ctx, "org")
		require.False(t, person, name)
		require.Equal(t, "org", id, name)
		require.Equal(t, map[string]string{"email": ""}, properties, name)
	}
}
