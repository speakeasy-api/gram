package feature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestPersonTarget(t *testing.T) {
	t.Parallel()

	session := func(email *string) *contextvalues.AuthContext {
		return &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", Email: email}
	}

	id, properties, ok := feature.PersonTarget(contextvalues.WithValidatedGramSession(t.Context(), session(new(" On@Example.com ")), false))
	require.True(t, ok)
	require.Equal(t, "on@example.com", id)
	require.Equal(t, map[string]string{"email": "on@example.com"}, properties)

	for name, ctx := range map[string]func() (string, map[string]string, bool){
		"no auth context": func() (string, map[string]string, bool) { return feature.PersonTarget(t.Context()) },
		"session no email": func() (string, map[string]string, bool) {
			return feature.PersonTarget(contextvalues.WithValidatedGramSession(t.Context(), session(nil), false))
		},
		"session blank": func() (string, map[string]string, bool) {
			return feature.PersonTarget(contextvalues.WithValidatedGramSession(t.Context(), session(new("  ")), false))
		},
		"api key": func() (string, map[string]string, bool) {
			return feature.PersonTarget(contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), session(new("on@example.com"))))
		},
	} {
		id, properties, ok := ctx()
		require.False(t, ok, name)
		require.Empty(t, id, name)
		require.Nil(t, properties, name)
	}
}
