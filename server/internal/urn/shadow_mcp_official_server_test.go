package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestShadowMCPOfficialServerRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewShadowMCPOfficialServer(id)

	require.Equal(t, "shadow_mcp_official_server:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseShadowMCPOfficialServer(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"shadow_mcp_official_server:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.ShadowMCPOfficialServer
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.ShadowMCPOfficialServer
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.ShadowMCPOfficialServer
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestShadowMCPOfficialServerRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseShadowMCPOfficialServer("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseShadowMCPOfficialServer("toolset:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseShadowMCPOfficialServer("shadow_mcp_official_server:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseShadowMCPOfficialServer("shadow_mcp_official_server:00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewShadowMCPOfficialServer(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewShadowMCPOfficialServer(uuid.Nil).MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)

	// Validation judges the value as it stands, not as it was constructed.
	mutated := urn.NewShadowMCPOfficialServer(uuid.MustParse("44444444-4444-4444-4444-444444444444"))
	mutated.ID = uuid.Nil
	_, err = mutated.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
