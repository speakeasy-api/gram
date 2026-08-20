package directory_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/directory"
)

func TestGroupPrincipalRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	principal := directory.GroupPrincipal(id)

	require.True(t, directory.IsGroupPrincipal(principal))
	require.False(t, directory.IsAttributePrincipal(principal))

	parsed, err := directory.ParseGroupPrincipal(principal)
	require.NoError(t, err)
	require.Equal(t, id, parsed)
}

func TestAttributePrincipalRoundTrip(t *testing.T) {
	t.Parallel()

	want := directory.AttributeValue{
		Key:   "manager:email/primary",
		Value: "lead+日本語@example.com:443",
	}
	principal := directory.AttributePrincipal(want.Key, want.Value)

	require.True(t, directory.IsAttributePrincipal(principal))
	require.False(t, directory.IsGroupPrincipal(principal))

	parsed, err := directory.ParseAttributePrincipal(principal)
	require.NoError(t, err)
	require.Equal(t, want, parsed)
}

func TestParseDirectoryPrincipalRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	tests := map[string]func() error{
		"group prefix": func() error {
			_, err := directory.ParseGroupPrincipal("role:" + uuid.NewString())
			return fmt.Errorf("parse group principal: %w", err)
		},
		"group id": func() error {
			_, err := directory.ParseGroupPrincipal("directory_group:not-a-uuid")
			return fmt.Errorf("parse group principal: %w", err)
		},
		"attribute prefix": func() error {
			_, err := directory.ParseAttributePrincipal("role:key:value")
			return fmt.Errorf("parse attribute principal: %w", err)
		},
		"attribute separator": func() error {
			_, err := directory.ParseAttributePrincipal("directory_attribute:a2V5")
			return fmt.Errorf("parse attribute principal: %w", err)
		},
		"attribute key": func() error {
			_, err := directory.ParseAttributePrincipal("directory_attribute:!:dmFsdWU")
			return fmt.Errorf("parse attribute principal: %w", err)
		},
		"attribute value": func() error {
			_, err := directory.ParseAttributePrincipal("directory_attribute:a2V5:!")
			return fmt.Errorf("parse attribute principal: %w", err)
		},
	}

	for name, parse := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := parse()
			require.ErrorIs(t, err, directory.ErrParsingPrincipal)
		})
	}
}
