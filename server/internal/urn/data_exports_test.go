package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestOtelDestinationRoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	original := urn.NewOtelDestination(id)
	require.Equal(t, "otel_destination:"+id.String(), original.String())

	parsed, err := urn.ParseOtelDestination(original.String())
	require.NoError(t, err)
	require.Equal(t, id, parsed.ID)

	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	var decoded urn.OtelDestination
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, id, decoded.ID)
}

func TestDataExportRouteRoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	original := urn.NewDataExportRoute(id)
	require.Equal(t, "data_export_route:"+id.String(), original.String())

	parsed, err := urn.ParseDataExportRoute(original.String())
	require.NoError(t, err)
	require.Equal(t, id, parsed.ID)

	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	var decoded urn.DataExportRoute
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, id, decoded.ID)
}
