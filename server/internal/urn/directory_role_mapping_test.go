package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDirectoryRoleMappingRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewDirectoryRoleMapping(id)

	require.Equal(t, "directory_role_mapping:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseDirectoryRoleMapping(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"directory_role_mapping:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.DirectoryRoleMapping
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.DirectoryRoleMapping
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.DirectoryRoleMapping
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestDirectoryRoleMappingRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseDirectoryRoleMapping("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDirectoryRoleMapping("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDirectoryRoleMapping("directory_role_mapping:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDirectoryRoleMapping("directory_role_mapping:" + uuid.Nil.String())
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewDirectoryRoleMapping(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
