package enrich

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

// The transform drops what a producer sends under the namespaces the
// pipeline writes, and nothing else: the pipeline's own keys and the
// directory enricher's, never a producer's own attributes or the gram keys
// Gram's own producers stamp before the transform.
func TestIsPipelineKeyCoversWhatThePipelineWrites(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		string(EventTypeColumnKey),
		string(OriginalInstrumentationScopeNameKey),
		string(OrganizationIDKey),
		string(TokensCountKey),
		string(TokensCodecKey),
		string(GramUserRolesKey),
		string(DirectoryIDKey),
		string(DirectoryGroupNamesKey),
		string(DirectoryAttribute("department_name")),
	} {
		require.True(t, IsPipelineKey(key), "%s is the pipeline's to write", key)
	}

	for _, key := range []string{
		"model",
		"gen_ai.usage.input_tokens",
		"gram.session.id",
		"directory",
		"speakeasy",
	} {
		require.False(t, IsPipelineKey(key), "%s is the producer's", key)
	}
}

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
