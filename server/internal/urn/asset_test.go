package urn_test

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

var testAssetUUIDv7 = uuid.MustParse("0195d3e0-89f7-7abc-9234-56789abcdef0")

func TestAssetRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewAsset(urn.AssetKindImage, testAssetUUIDv7)

	require.Equal(t, "assets:image:0195d3e0-89f7-7abc-9234-56789abcdef0", original.String())

	parsed, err := urn.ParseAsset(original.String())
	require.NoError(t, err)
	require.Equal(t, original.Kind, parsed.Kind)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"assets:image:0195d3e0-89f7-7abc-9234-56789abcdef0"`, string(data))

	var fromJSON urn.Asset
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromJSON.Kind)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Asset
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromText.Kind)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Asset
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Kind, fromDB.Kind)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestAssetSupportsAllKinds(t *testing.T) {
	t.Parallel()

	require.Equal(t, "assets:image:0195d3e0-89f7-7abc-9234-56789abcdef0", urn.NewAsset(urn.AssetKindImage, testAssetUUIDv7).String())
	require.Equal(t, "assets:function:0195d3e0-89f7-7abc-9234-56789abcdef0", urn.NewAsset(urn.AssetKindFunction, testAssetUUIDv7).String())
	require.Equal(t, "assets:openapi:0195d3e0-89f7-7abc-9234-56789abcdef0", urn.NewAsset(urn.AssetKindOpenAPI, testAssetUUIDv7).String())
}

func TestAssetRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseAsset("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAsset("asset:image:0195d3e0-89f7-7abc-9234-56789abcdef0")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAsset("assets:invalid:0195d3e0-89f7-7abc-9234-56789abcdef0")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAsset("assets:image:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAsset("assets:image:11111111-1111-4111-8111-111111111111")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAsset(urn.AssetKindImage, uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAsset(urn.AssetKind("invalid"), testAssetUUIDv7).MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestAssetScanNil(t *testing.T) {
	t.Parallel()

	var got urn.Asset
	err := got.Scan(nil)
	require.NoError(t, err)
	require.True(t, got.IsZero())
}

func TestAssetScanUnsupportedType(t *testing.T) {
	t.Parallel()

	var got urn.Asset
	err := got.Scan(123)
	require.Error(t, err)
}

func TestAssetValueType(t *testing.T) {
	t.Parallel()

	value, err := urn.NewAsset(urn.AssetKindFunction, testAssetUUIDv7).Value()
	require.NoError(t, err)
	require.Equal(t, driver.Value("assets:function:0195d3e0-89f7-7abc-9234-56789abcdef0"), value)
}
