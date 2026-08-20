package assistants

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandleConsultToolCallUnauthorizedWithoutToken(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_consult_http_unauth")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
	svc := &Service{logger: logger, core: core}

	req := httptest.NewRequest(http.MethodPost, "/rpc/assistants.consultToolCall", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	err = svc.handleConsultToolCall(rec, req)
	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeUnauthorized, se.Code)
}
