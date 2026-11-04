package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestIssuanceSelectionProfileRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewIssuanceSelectionProfile(id)

	require.Equal(t, "issuance_selection_profile:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseIssuanceSelectionProfile(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"issuance_selection_profile:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.IssuanceSelectionProfile
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.IssuanceSelectionProfile
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.IssuanceSelectionProfile
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestIssuanceSelectionProfileRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseIssuanceSelectionProfile("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseIssuanceSelectionProfile("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseIssuanceSelectionProfile("issuance_selection_profile:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseIssuanceSelectionProfile("issuance_selection_profile:" + uuid.Nil.String())
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewIssuanceSelectionProfile(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
