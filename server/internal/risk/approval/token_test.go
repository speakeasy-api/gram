package approval_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/approval"
)

func TestNewTokenShape(t *testing.T) {
	t.Parallel()

	tok, err := approval.NewToken()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(tok, approval.TokenPrefix))
	require.Len(t, tok, len(approval.TokenPrefix)+43)
	require.True(t, approval.IsToken(tok))
}

func TestNewTokenIsUnique(t *testing.T) {
	t.Parallel()

	a, err := approval.NewToken()
	require.NoError(t, err)
	b, err := approval.NewToken()
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestIsTokenRejectsOtherShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "prefix only", input: approval.TokenPrefix},
		{name: "wrong prefix", input: "awt_" + strings.Repeat("A", 43)},
		{name: "too short", input: approval.TokenPrefix + strings.Repeat("A", 42)},
		{name: "not base64url", input: approval.TokenPrefix + strings.Repeat("+", 43)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, approval.IsToken(tc.input))
		})
	}
}
