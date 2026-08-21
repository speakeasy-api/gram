package otelforwarding

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/otel_forwarding"
)

func TestNormalizeHeaderInputsPreservesOmittedExistingValue(t *testing.T) {
	t.Parallel()

	newValue := "happy days"

	headers, err := normalizeHeaderInputs(
		[]*gen.OtelForwardingHeaderInput{
			{Name: "foo", Value: nil},
			{Name: "another", Value: &newValue},
		},
		map[string]string{"foo": "original secret"},
	)

	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"another": "happy days",
		"foo":     "original secret",
	}, headers)
}

func TestNormalizeHeaderInputsRemovesOmittedHeaderEntry(t *testing.T) {
	t.Parallel()

	headers, err := normalizeHeaderInputs(
		[]*gen.OtelForwardingHeaderInput{{Name: "kept", Value: nil}},
		map[string]string{
			"kept":    "kept secret",
			"removed": "removed secret",
		},
	)

	require.NoError(t, err)
	require.Equal(t, map[string]string{"kept": "kept secret"}, headers)
}

func TestNormalizeHeaderInputsRejectsOmittedNewValue(t *testing.T) {
	t.Parallel()

	headers, err := normalizeHeaderInputs(
		[]*gen.OtelForwardingHeaderInput{{Name: "new", Value: nil}},
		nil,
	)

	require.Nil(t, headers)
	require.ErrorContains(t, err, `header value is required for new header: "new"`)
}

func TestNormalizeHeaderInputsAcceptsExplicitEmptyValue(t *testing.T) {
	t.Parallel()

	emptyValue := ""

	headers, err := normalizeHeaderInputs(
		[]*gen.OtelForwardingHeaderInput{{Name: "existing", Value: &emptyValue}},
		map[string]string{"existing": "original secret"},
	)

	require.NoError(t, err)
	require.Equal(t, map[string]string{"existing": ""}, headers)
}
