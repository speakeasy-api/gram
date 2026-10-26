package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestCatalogPromotionRoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	original := urn.NewCatalogPromotion(id)
	parsed, err := urn.ParseCatalogPromotion(original.String())
	require.NoError(t, err)
	require.Equal(t, id, parsed.ID)
	data, err := json.Marshal(original)
	require.NoError(t, err)
	var decoded urn.CatalogPromotion
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, id, decoded.ID)
	text, err := original.MarshalText()
	require.NoError(t, err)
	require.NoError(t, decoded.UnmarshalText(text))
	require.Equal(t, id, decoded.ID)
	value, err := original.Value()
	require.NoError(t, err)
	require.NoError(t, decoded.Scan(value))
	require.Equal(t, id, decoded.ID)
	require.NoError(t, decoded.Scan([]byte(original.String())))
	require.Equal(t, id, decoded.ID)
}

func TestCatalogPromotionRejectsInvalidInputsAndCurrentNilID(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "catalog-promotion:not-a-uuid", "other:" + uuid.NewString(), "catalog-promotion:" + uuid.Nil.String()} {
		_, err := urn.ParseCatalogPromotion(value)
		require.ErrorIs(t, err, urn.ErrInvalid)
		var decoded urn.CatalogPromotion
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.ErrorIs(t, json.Unmarshal(encoded, &decoded), urn.ErrInvalid)
		require.ErrorIs(t, decoded.UnmarshalText([]byte(value)), urn.ErrInvalid)
		require.ErrorIs(t, decoded.Scan(value), urn.ErrInvalid)
	}
	current := urn.NewCatalogPromotion(uuid.New())
	current.ID = uuid.Nil
	_, err := current.MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = current.MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = current.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
	// Correcting an initially invalid value also uses its current identifier.
	current = urn.NewCatalogPromotion(uuid.Nil)
	current.ID = uuid.New()
	_, err = current.Value()
	require.NoError(t, err)
}
