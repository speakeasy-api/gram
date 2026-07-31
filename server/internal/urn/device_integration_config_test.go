package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDeviceIntegrationConfigRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewDeviceIntegrationConfig(id)

	require.Equal(t, "device_integration_config:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseDeviceIntegrationConfig(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"device_integration_config:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.DeviceIntegrationConfig
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.DeviceIntegrationConfig
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.DeviceIntegrationConfig
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestDeviceIntegrationConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseDeviceIntegrationConfig("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDeviceIntegrationConfig("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDeviceIntegrationConfig("device_integration_config:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewDeviceIntegrationConfig(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
