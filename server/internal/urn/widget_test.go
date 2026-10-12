package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestWidgetRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewWidget(id)

	require.Equal(t, "widget:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseWidget(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"widget:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.Widget
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.Widget
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.Widget
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestWidgetRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseWidget("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseWidget("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseWidget("widget:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseWidget("widget:00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewWidget(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
