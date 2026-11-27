package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestOrganizationEncryptionConfigRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewOrganizationEncryptionConfig(id)

	require.Equal(t, "organization_encryption_config:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseOrganizationEncryptionConfig(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"organization_encryption_config:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.OrganizationEncryptionConfig
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.OrganizationEncryptionConfig
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.OrganizationEncryptionConfig
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestOrganizationEncryptionConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseOrganizationEncryptionConfig("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseOrganizationEncryptionConfig("aws_kms_key:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseOrganizationEncryptionConfig("organization_encryption_config:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewOrganizationEncryptionConfig(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestParseOrganizationEncryptionConfig_RejectsNilID(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseOrganizationEncryptionConfig("organization_encryption_config:00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, urn.ErrInvalid)
}
