package auditapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	goahttp "goa.design/goa/v3/http"

	auditHTTP "github.com/speakeasy-api/gram/server/gen/http/auditlogs/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/stretchr/testify/require"
)

func TestInternalClassificationResponse(t *testing.T) {
	t.Parallel()
	for _, internal := range []bool{false, true} {
		log, err := toAuditLog(repo.ListAuditLogsRow{IsInternal: internal}, audit.CategoryConfiguration)
		require.NoError(t, err)
		require.NotNil(t, log.IsInternal)
		require.Equal(t, internal, *log.IsInternal)
		recorder := httptest.NewRecorder()
		result := &gen.ListAuditLogsResult{Logs: []*gen.AuditLog{log}, NextCursor: nil}
		require.NoError(t, auditHTTP.EncodeListResponse(goahttp.ResponseEncoder)(t.Context(), recorder, result))
		require.Equal(t, http.StatusOK, recorder.Code)
		var body struct {
			Logs []map[string]any `json:"logs"`
		}
		require.NoError(t, json.NewDecoder(recorder.Body).Decode(&body))
		require.Len(t, body.Logs, 1)
		require.Contains(t, body.Logs[0], "is_internal")
		require.Equal(t, internal, body.Logs[0]["is_internal"])
	}
}
