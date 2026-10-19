package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDirectoryReviewItemRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewDirectoryReviewItem(id)

	require.Equal(t, "directory_review_item:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseDirectoryReviewItem(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"directory_review_item:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.DirectoryReviewItem
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.DirectoryReviewItem
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.DirectoryReviewItem
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestDirectoryReviewItemRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseDirectoryReviewItem("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDirectoryReviewItem("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDirectoryReviewItem("directory_review_item:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDirectoryReviewItem("directory_review_item:" + uuid.Nil.String())
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewDirectoryReviewItem(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
