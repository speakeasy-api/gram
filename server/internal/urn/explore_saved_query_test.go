package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestExploreSavedQueryRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewExploreSavedQuery(id)
	require.Equal(t, "explore_saved_query:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseExploreSavedQuery(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var fromJSON urn.ExploreSavedQuery
	require.NoError(t, json.Unmarshal(data, &fromJSON))
	require.Equal(t, original.ID, fromJSON.ID)

	value, err := original.Value()
	require.NoError(t, err)
	var fromDB urn.ExploreSavedQuery
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original.ID, fromDB.ID)
}

func TestExploreSavedQueryRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseExploreSavedQuery("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseExploreSavedQuery("skill:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewExploreSavedQuery(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
