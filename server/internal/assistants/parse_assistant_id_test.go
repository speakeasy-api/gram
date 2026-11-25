package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestParseAssistantID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	got, err := parseAssistantID(id.String())
	require.NoError(t, err)
	require.Equal(t, id, got)

	for name, raw := range map[string]string{
		"nil uuid": uuid.Nil.String(),
		"empty":    "",
		"not uuid": "not-a-uuid",
		"wildcard": "*",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseAssistantID(raw)
			require.Error(t, err)
		})
	}
}
