package enforcereply

import (
	"testing"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
)

// A command failing inside the MULTI/EXEC (here RPUSH against a key holding
// the wrong type) must surface through TxPipelined's returned error rather
// than pass silently.
func TestWriterSurfacesInTransactionCommandFailure(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-writer-txn")
	require.NoError(t, te.client.Set(t.Context(), InboxKey("replica-writer-txn"), "scalar", 0).Err())

	err := te.writer.Reply(t.Context(), te.inbox.URN("correlation-txn"), testReply("correlation-txn", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	require.Error(t, err)
	require.ErrorContains(t, err, "write reply")
}
