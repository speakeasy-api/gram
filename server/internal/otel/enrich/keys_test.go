package enrich

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// The canonical copy of a sensitive value is as sensitive as its source: a
// destination that excludes sensitive data must not receive a prompt's
// words or a person's identity through the speakeasy.agent keys. The
// dialect package spells those keys itself, since it cannot import this
// one, so this pins the two spellings together.
func TestCanonicalCopiesOfSensitiveValuesAreSensitive(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		string(TextColumnKey),
		string(UserEmailColumnKey),
		string(ExternalUserIDColumnKey),
	} {
		require.True(t, dialect.IsSensitiveDataKey(key), "%s must be redacted for an exclude destination", key)
	}

	// The keys that carry no words and no person stay visible, so a
	// destination that excludes sensitive data still gets the event's shape.
	for _, key := range []string{
		string(EventTypeColumnKey),
		string(SessionIDColumnKey),
		string(ModelColumnKey),
		string(ToolNameColumnKey),
		string(OutcomeColumnKey),
	} {
		require.False(t, dialect.IsSensitiveDataKey(key), "%s is not sensitive", key)
	}
}
