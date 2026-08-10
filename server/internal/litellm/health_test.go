package litellm

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHealthProcessorCoalescesUpdates(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	apiKeyID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_test",
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              apiKeyID.String(),
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})
	var mu sync.Mutex
	writes := make([]repo.RecordLiteLLMInstanceHealthParams, 0, 1)
	processor := newHealthProcessor(testenv.NewLogger(t), time.Hour, func(_ context.Context, params repo.RecordLiteLLMInstanceHealthParams) error {
		mu.Lock()
		defer mu.Unlock()
		writes = append(writes, params)
		return nil
	})
	processor.Start(ctx)
	processor.Record(ctx, healthSignalOTEL, "1.93.0", oops.C(oops.CodeBadRequest))
	processor.Record(ctx, healthSignalGuardrail, "1.94.0", nil)
	processor.Record(ctx, healthSignalOTEL, "1.95.0", oops.C(oops.CodeRequestTooLarge))
	require.NoError(t, processor.Shutdown(t.Context()))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, writes, 1)
	require.True(t, writes[0].GuardrailObservedAt.Valid)
	require.True(t, writes[0].OtelObservedAt.Valid)
	require.True(t, writes[0].ErrorObservedAt.Valid)
	require.Equal(t, writes[0].OtelObservedAt.Time, writes[0].ErrorObservedAt.Time)
	require.True(t, writes[0].ReportedVersionObservedAt.Valid)
	require.Equal(t, "limit_exceeded", writes[0].ErrorKind)
	require.Equal(t, "1.95.0", writes[0].ReportedLitellmVersion)
	require.Equal(t, "org_test", writes[0].OrganizationID)
	require.Equal(t, projectID, writes[0].ProjectID)
	require.Equal(t, apiKeyID, writes[0].ApiKeyID)
}

func TestHealthProcessorRecordsRecoveryAfterError(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	apiKeyID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_test",
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              apiKeyID.String(),
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})
	writes := make([]repo.RecordLiteLLMInstanceHealthParams, 0, 1)
	processor := newHealthProcessor(testenv.NewLogger(t), time.Hour, func(_ context.Context, params repo.RecordLiteLLMInstanceHealthParams) error {
		writes = append(writes, params)
		return nil
	})
	processor.Start(ctx)
	processor.Record(ctx, healthSignalOTEL, "1.94.0", oops.C(oops.CodeBadRequest))
	processor.Record(ctx, healthSignalGuardrail, "1.94.0", nil)
	require.NoError(t, processor.Shutdown(t.Context()))

	require.Len(t, writes, 1)
	require.Equal(t, "decode_failure", writes[0].ErrorKind)
	require.True(t, writes[0].GuardrailObservedAt.Valid)
	require.True(t, writes[0].OtelObservedAt.Valid)
	require.True(t, writes[0].ErrorObservedAt.Valid)
	require.True(t, writes[0].ReportedVersionObservedAt.Valid)
	require.True(t, writes[0].GuardrailObservedAt.Time.After(writes[0].ErrorObservedAt.Time))
}

func TestClassifyHealthErrorUsesFixedCategories(t *testing.T) {
	t.Parallel()

	require.Equal(t, healthErrorAuthFailure, classifyHealthError(oops.C(oops.CodeUnauthorized)))
	require.Equal(t, healthErrorAuthFailure, classifyHealthError(oops.C(oops.CodeForbidden)))
	require.Equal(t, healthErrorDecode, classifyHealthError(oops.C(oops.CodeBadRequest)))
	require.Equal(t, healthErrorDecode, classifyHealthError(oops.C(oops.CodeUnsupportedMedia)))
	require.Equal(t, healthErrorLimitExceeded, classifyHealthError(oops.C(oops.CodeRequestTooLarge)))
	require.Equal(t, healthErrorNone, classifyHealthError(oops.C(oops.CodeUnexpected)))
}

func TestManagedInstanceIngestUpdatesHealth(t *testing.T) {
	t.Parallel()

	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "health", FailurePosture: "fail_closed"})
	require.NoError(t, err)

	keyScheme := &security.APIKeyScheme{Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	projectScheme := &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	ingestCtx, err := ti.service.APIKeyAuth(t.Context(), created.Key, keyScheme)
	require.NoError(t, err)
	ingestCtx, err = ti.service.APIKeyAuth(ingestCtx, string(created.Instance.Project.Slug), projectScheme)
	require.NoError(t, err)

	payload := testPayload()
	payload.LitellmVersion = new("1.94.0")
	result, err := ti.service.Ingest(ingestCtx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	otelBody := []byte(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"litellm"}},{"key":"service.version","value":{"stringValue":"1.95.0"}}]},"scopeSpans":[]}]}`)
	response := serveTraceRequest(t, mountedTraceMux(ti.service), otelBody, "application/json", "", created.Key, string(created.Instance.Project.Slug))
	require.Equal(t, http.StatusAccepted, response.Code)

	invalid := testPayload()
	invalid.InputType = "invalid"
	_, err = ti.service.Ingest(ingestCtx, invalid)
	requireOops(t, err, oops.CodeBadRequest)

	instanceID, err := uuid.Parse(created.Instance.ID)
	require.NoError(t, err)
	queries := repo.New(ti.conn)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		instance, queryErr := queries.GetLiteLLMInstanceForUpdate(ctx, repo.GetLiteLLMInstanceForUpdateParams{
			ID:             instanceID,
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		assert.NoError(collect, queryErr)
		assert.True(collect, instance.LastGuardrailEventAt.Valid)
		assert.True(collect, instance.LastOtelEventAt.Valid)
		assert.True(collect, instance.LastErrorAt.Valid)
		assert.Equal(collect, "decode_failure", instance.LastErrorKind.String)
		assert.Equal(collect, "1.95.0", instance.ReportedLitellmVersion.String)
	}, 5*time.Second, 20*time.Millisecond)
}

func TestHealthProcessorPersistsAcceptedUpdatesAcrossLifecycleChanges(t *testing.T) {
	t.Parallel()

	ctx, ti := newRealTestService(t, nil)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "health-lifecycle", FailurePosture: "fail_closed"})
	require.NoError(t, err)

	keyScheme := &security.APIKeyScheme{Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	projectScheme := &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	oldKeyCtx, err := ti.service.APIKeyAuth(t.Context(), created.Key, keyScheme)
	require.NoError(t, err)
	oldKeyCtx, err = ti.service.APIKeyAuth(oldKeyCtx, string(created.Instance.Project.Slug), projectScheme)
	require.NoError(t, err)
	ti.service.health.Record(oldKeyCtx, healthSignalGuardrail, "1.94.0", nil)

	rotated, err := ti.service.RotateInstanceKey(ctx, &gen.RotateInstanceKeyPayload{ID: created.Instance.ID})
	require.NoError(t, err)
	newKeyCtx, err := ti.service.APIKeyAuth(t.Context(), rotated.Key, keyScheme)
	require.NoError(t, err)
	newKeyCtx, err = ti.service.APIKeyAuth(newKeyCtx, string(created.Instance.Project.Slug), projectScheme)
	require.NoError(t, err)
	ti.service.health.Record(newKeyCtx, healthSignalOTEL, "1.95.0", oops.C(oops.CodeBadRequest))
	require.NoError(t, ti.service.RevokeInstance(ctx, &gen.RevokeInstancePayload{ID: created.Instance.ID}))
	require.NoError(t, ti.service.health.Shutdown(t.Context()))

	listed, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	instance := instanceByName(t, listed, "health-lifecycle")
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("failed"), instance.Diagnostics.Status)
	require.NotNil(t, instance.Diagnostics.LastGuardrailEventAt)
	require.NotNil(t, instance.Diagnostics.LastOtelEventAt)
	require.NotNil(t, instance.Diagnostics.LastErrorAt)
	require.Equal(t, "1.95.0", *instance.Diagnostics.ReportedLitellmVersion)
}

func TestReportedVersionsIgnoreOtherOTLPServices(t *testing.T) {
	t.Parallel()

	traceRequest, err := decodeOTLPJSON([]byte(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"other-service"}},{"key":"service.version","value":{"stringValue":"9.9.9"}}]}},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"litellm"}},{"key":"service.version","value":{"stringValue":"1.94.0"}}]}}]}`))
	require.NoError(t, err)
	require.Equal(t, "1.94.0", traceReportedVersion(traceRequest))
	traceRequest.ResourceSpans = traceRequest.ResourceSpans[:1]
	require.Empty(t, traceReportedVersion(traceRequest))

	metricRequest, err := decodeMetricExport([]byte(`{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"other-service"}},{"key":"service.version","value":{"stringValue":"9.9.9"}}]}},{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"litellm"}},{"key":"service.version","value":{"stringValue":"1.94.0"}}]}}]}`), "application/json")
	require.NoError(t, err)
	require.Equal(t, "1.94.0", metricReportedVersion(metricRequest))
	metricRequest.ResourceMetrics = metricRequest.ResourceMetrics[:1]
	require.Empty(t, metricReportedVersion(metricRequest))
}
