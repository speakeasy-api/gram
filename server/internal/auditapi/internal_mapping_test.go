package auditapi

import (
	"encoding/json"
	"testing"

	auditHTTP "github.com/speakeasy-api/gram/server/gen/http/auditlogs/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/stretchr/testify/require"
)

func TestInternalClassificationResponse(t *testing.T) {
	for _, internal := range []bool{false, true} {
		log, err := toAuditLog(repo.ListAuditLogsRow{IsInternal: internal}, audit.CategoryConfiguration)
		require.NoError(t, err)
		require.Equal(t, internal, log.IsInternal)
		body, err := json.Marshal(auditHTTP.AuditLogResponseBody{IsInternal: log.IsInternal})
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Contains(t, fields, "is_internal")
		require.Equal(t, internal, fields["is_internal"])
	}
}
