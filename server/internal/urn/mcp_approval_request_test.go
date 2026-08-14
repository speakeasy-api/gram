package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestMCPApprovalRequestRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewMCPApprovalRequest(id)

	require.Equal(t, "mcp-approval-request:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseMCPApprovalRequest(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"mcp-approval-request:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.MCPApprovalRequest
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.MCPApprovalRequest
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.MCPApprovalRequest
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestMCPApprovalRequestRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseMCPApprovalRequest("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMCPApprovalRequest("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMCPApprovalRequest("mcp-approval-request:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewMCPApprovalRequest(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseMCPApprovalRequest("mcp-approval-request:00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// Validation judges the value as it stands, not as it was constructed.
	mutated := urn.NewMCPApprovalRequest(uuid.MustParse("33333333-3333-3333-3333-333333333333"))
	mutated.ID = uuid.Nil
	_, err = mutated.MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
