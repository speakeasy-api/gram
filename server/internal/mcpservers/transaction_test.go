package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
)

// A missing transaction is refused as an error before the ownership queries
// run, rather than panicking on the first of them.
func TestCreateProjectMCPServerInTransactionRefusesMissingInput(t *testing.T) {
	t.Parallel()

	_, err := mcpservers.CreateProjectMCPServerInTransaction(t.Context(), nil, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID: "org", ProjectID: uuid.New(), ActorUserID: "user", Name: "Server",
		Visibility: mcpservers.VisibilityPrivate, ToolsetID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	})
	require.Error(t, err)
}
