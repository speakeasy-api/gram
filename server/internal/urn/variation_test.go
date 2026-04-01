package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

var testVariationUUIDv7 = uuid.MustParse("0195d3e0-89f7-7abc-9234-56789abcdef1")

func TestVariationRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewVariation(urn.VariationKindGlobal, testVariationUUIDv7)

	require.Equal(t, "variations:global:0195d3e0-89f7-7abc-9234-56789abcdef1", original.String())

	parsed, err := urn.ParseVariation(original.String())
	require.NoError(t, err)
	require.Equal(t, original.Kind, parsed.Kind)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"variations:global:0195d3e0-89f7-7abc-9234-56789abcdef1"`, string(data))

	var fromJSON urn.Variation
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromJSON.Kind)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Variation
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromText.Kind)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Variation
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromDB.Kind)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestVariationSupportsAllKinds(t *testing.T) {
	t.Parallel()

	require.Equal(t, "variations:global:0195d3e0-89f7-7abc-9234-56789abcdef1", urn.NewVariation(urn.VariationKindGlobal, testVariationUUIDv7).String())
	require.Equal(t, "variations:toolset:0195d3e0-89f7-7abc-9234-56789abcdef1", urn.NewVariation(urn.VariationKindToolset, testVariationUUIDv7).String())
}

func TestVariationRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseVariation("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseVariation("variation:global:0195d3e0-89f7-7abc-9234-56789abcdef1")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseVariation("variations:invalid:0195d3e0-89f7-7abc-9234-56789abcdef1")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseVariation("variations:global:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseVariation("variations:global:11111111-1111-4111-8111-111111111111")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewVariation(urn.VariationKindGlobal, uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewVariation(urn.VariationKind("invalid"), testVariationUUIDv7).MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestVariationScanNil(t *testing.T) {
	t.Parallel()

	var got urn.Variation
	err := got.Scan(nil)
	require.NoError(t, err)
	require.True(t, got.IsZero())
}

func TestVariationScanUnsupportedType(t *testing.T) {
	t.Parallel()

	var got urn.Variation
	err := got.Scan(123)
	require.Error(t, err)
}

func TestVariationValueType(t *testing.T) {
	t.Parallel()

	value, err := urn.NewVariation(urn.VariationKindToolset, testVariationUUIDv7).Value()
	require.NoError(t, err)
	require.Equal(t, driver.Value("variations:toolset:0195d3e0-89f7-7abc-9234-56789abcdef1"), value)
}
