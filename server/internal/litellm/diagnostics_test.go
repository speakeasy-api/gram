package litellm

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	litellmrepo "github.com/speakeasy-api/gram/server/internal/litellm/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDeriveInstanceHealthStatus(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	earlier := pgtype.Timestamptz{Time: now.Add(-time.Minute), InfinityModifier: pgtype.Finite, Valid: true}
	later := pgtype.Timestamptz{Time: now, InfinityModifier: pgtype.Finite, Valid: true}
	empty := pgtype.Timestamptz{}

	require.Equal(t, gen.LiteLLMInstanceHealthStatus("pending"), deriveInstanceHealthStatus(empty, empty, empty))
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("success"), deriveInstanceHealthStatus(later, empty, empty))
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("failed"), deriveInstanceHealthStatus(empty, empty, later))
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("failed"), deriveInstanceHealthStatus(earlier, empty, later))
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("success"), deriveInstanceHealthStatus(later, empty, earlier))
}

func TestListInstancesIncludesSafeDiagnostics(t *testing.T) {
	t.Parallel()

	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "with-traffic", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("pending"), created.Instance.Diagnostics.Status)
	require.Nil(t, created.Instance.Diagnostics.VirtualKeyEmailPct24h)
	require.Nil(t, created.Instance.Diagnostics.PlatformUserPct24h)
	withoutTraffic, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "without-traffic", FailurePosture: "fail_open"})
	require.NoError(t, err)

	keyHash, err := auth.GetAPIKeyHash(created.Key)
	require.NoError(t, err)
	managedKey, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.NoError(t, err)
	instanceID, err := uuid.Parse(created.Instance.ID)
	require.NoError(t, err)
	failedAt := time.Now().UTC()
	require.NoError(t, litellmrepo.New(ti.conn).RecordLiteLLMInstanceHealth(ctx, litellmrepo.RecordLiteLLMInstanceHealthParams{
		GuardrailObservedAt:       conv.ToPGTimestamptz(failedAt),
		OtelObservedAt:            conv.PtrToPGTimestamptz(nil),
		ErrorObservedAt:           conv.ToPGTimestamptz(failedAt),
		ErrorKind:                 "decode_failure",
		ReportedLitellmVersion:    "1.94.0",
		ReportedVersionObservedAt: conv.ToPGTimestamptz(failedAt),
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		InstanceID:                uuid.NullUUID{UUID: instanceID, Valid: true},
		ApiKeyID:                  managedKey.ID,
	}))
	keyScheme := &security.APIKeyScheme{Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	projectScheme := &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	ingestCtx, err := ti.service.APIKeyAuth(t.Context(), created.Key, keyScheme)
	require.NoError(t, err)
	ingestCtx, err = ti.service.APIKeyAuth(ingestCtx, string(created.Instance.Project.Slug), projectScheme)
	require.NoError(t, err)
	coldResolver := NewInstanceResolver(testenv.NewLogger(t), ti.conn)
	resolvedInstanceID, managed := coldResolver.Resolve(ingestCtx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), managedKey.ID.String())
	require.True(t, managed)
	require.Equal(t, instanceID, resolvedInstanceID)

	rotated, err := ti.service.RotateInstanceKey(ctx, &gen.RotateInstanceKeyPayload{ID: created.Instance.ID})
	require.NoError(t, err)
	require.NotEqual(t, created.Key, rotated.Key)
	rotatedHash, err := auth.GetAPIKeyHash(rotated.Key)
	require.NoError(t, err)
	rotatedKey, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, rotatedHash)
	require.NoError(t, err)

	now := time.Now().UTC()
	rows := []telemetryrepo.InsertTelemetryLogParams{
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now, "call-1", "member@example.test", "user-1", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now, "call-2", "unknown@example.test", "", liteLLMEventURN("embeddings")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now, "call-3", "", "user-2", liteLLMEventURN("text_completion")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now, "call-4", "", "", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now, "call-1", "member@example.test", "user-1", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, rotatedKey.ID, now, "call-5", "member@example.test", "user-3", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, rotatedKey.ID, now, "call-6", "", "", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now, "ignored-operational", "member@example.test", "user-1", liteLLMEventURN("unknown")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, instanceID, managedKey.ID, now.Add(-25*time.Hour), "ignored-old", "member@example.test", "user-1", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, *authCtx.ProjectID, uuid.New(), uuid.New(), now, "ignored-instance", "member@example.test", "user-1", liteLLMEventURN("chat")),
		liteLLMTrafficLog(t, uuid.New(), instanceID, managedKey.ID, now, "ignored-project", "member@example.test", "user-1", liteLLMEventURN("chat")),
	}
	require.NoError(t, telemetryrepo.New(ti.chConn).InsertTelemetryLogs(ctx, rows))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	var diagnostics *gen.LiteLLMInstanceDiagnostics
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		listed, listErr := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
		assert.NoError(collect, listErr)
		if listErr != nil {
			return
		}
		withTraffic := instanceByNameCollect(collect, listed, "with-traffic")
		noTraffic := instanceByNameCollect(collect, listed, "without-traffic")
		if withTraffic == nil || noTraffic == nil {
			return
		}
		diagnostics = withTraffic.Diagnostics
		assert.NotNil(collect, diagnostics)
		if diagnostics != nil {
			assert.Equal(collect, gen.LiteLLMInstanceHealthStatus("failed"), diagnostics.Status)
			assert.NotNil(collect, diagnostics.VirtualKeyEmailPct24h)
			assert.NotNil(collect, diagnostics.PlatformUserPct24h)
			if diagnostics.VirtualKeyEmailPct24h != nil && diagnostics.PlatformUserPct24h != nil {
				assert.InDelta(collect, 50.0, *diagnostics.VirtualKeyEmailPct24h, 0.01)
				assert.InDelta(collect, 50.0, *diagnostics.PlatformUserPct24h, 0.01)
			}
		}
		assert.NotNil(collect, noTraffic.Diagnostics)
		if noTraffic.Diagnostics != nil {
			assert.Equal(collect, gen.LiteLLMInstanceHealthStatus("pending"), noTraffic.Diagnostics.Status)
			assert.Nil(collect, noTraffic.Diagnostics.VirtualKeyEmailPct24h)
			assert.Nil(collect, noTraffic.Diagnostics.PlatformUserPct24h)
		}
	}, 5*time.Second, 20*time.Millisecond)

	require.NotNil(t, diagnostics)
	require.NotNil(t, diagnostics.LastGuardrailEventAt)
	require.NotNil(t, diagnostics.LastErrorAt)
	require.Equal(t, gen.LiteLLMInstanceErrorKind("decode_failure"), *diagnostics.LastErrorKind)
	require.Equal(t, "1.94.0", *diagnostics.ReportedLitellmVersion)
	view := fmt.Sprintf("%+v", diagnostics)
	require.NotContains(t, view, created.Key)
	require.NotContains(t, view, managedKey.ID.String())
	require.NotContains(t, view, "member@example.test")
	require.NotContains(t, view, "unknown@example.test")
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("pending"), withoutTraffic.Instance.Diagnostics.Status)

	recoveredAt := failedAt.Add(time.Second)
	require.NoError(t, litellmrepo.New(ti.conn).RecordLiteLLMInstanceHealth(ctx, litellmrepo.RecordLiteLLMInstanceHealthParams{
		GuardrailObservedAt:       conv.ToPGTimestamptz(recoveredAt),
		OtelObservedAt:            conv.PtrToPGTimestamptz(nil),
		ErrorObservedAt:           conv.PtrToPGTimestamptz(nil),
		ErrorKind:                 "",
		ReportedLitellmVersion:    "",
		ReportedVersionObservedAt: conv.PtrToPGTimestamptz(nil),
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		InstanceID:                uuid.NullUUID{UUID: instanceID, Valid: true},
		ApiKeyID:                  rotatedKey.ID,
	}))
	listedAfterRecovery, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("success"), instanceByName(t, listedAfterRecovery, "with-traffic").Diagnostics.Status)

	relapsedAt := recoveredAt.Add(time.Second)
	require.NoError(t, litellmrepo.New(ti.conn).RecordLiteLLMInstanceHealth(ctx, litellmrepo.RecordLiteLLMInstanceHealthParams{
		GuardrailObservedAt:       conv.PtrToPGTimestamptz(nil),
		OtelObservedAt:            conv.PtrToPGTimestamptz(nil),
		ErrorObservedAt:           conv.ToPGTimestamptz(relapsedAt),
		ErrorKind:                 "decode_failure",
		ReportedLitellmVersion:    "",
		ReportedVersionObservedAt: conv.PtrToPGTimestamptz(nil),
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		InstanceID:                uuid.NullUUID{UUID: instanceID, Valid: true},
		ApiKeyID:                  rotatedKey.ID,
	}))
	listedAfterRelapse, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("failed"), instanceByName(t, listedAfterRelapse, "with-traffic").Diagnostics.Status)

	require.NoError(t, litellmrepo.New(ti.conn).RecordLiteLLMInstanceHealth(ctx, litellmrepo.RecordLiteLLMInstanceHealthParams{
		GuardrailObservedAt:       conv.ToPGTimestamptz(recoveredAt.Add(500 * time.Millisecond)),
		OtelObservedAt:            conv.PtrToPGTimestamptz(nil),
		ErrorObservedAt:           conv.PtrToPGTimestamptz(nil),
		ErrorKind:                 "",
		ReportedLitellmVersion:    "",
		ReportedVersionObservedAt: conv.PtrToPGTimestamptz(nil),
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		InstanceID:                uuid.NullUUID{UUID: instanceID, Valid: true},
		ApiKeyID:                  rotatedKey.ID,
	}))
	listedAfterStaleRecovery, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("failed"), instanceByName(t, listedAfterStaleRecovery, "with-traffic").Diagnostics.Status)

	finalRecoveryAt := relapsedAt.Add(time.Second)
	require.NoError(t, litellmrepo.New(ti.conn).RecordLiteLLMInstanceHealth(ctx, litellmrepo.RecordLiteLLMInstanceHealthParams{
		GuardrailObservedAt:       conv.ToPGTimestamptz(finalRecoveryAt),
		OtelObservedAt:            conv.PtrToPGTimestamptz(nil),
		ErrorObservedAt:           conv.PtrToPGTimestamptz(nil),
		ErrorKind:                 "",
		ReportedLitellmVersion:    "1.95.0",
		ReportedVersionObservedAt: conv.ToPGTimestamptz(finalRecoveryAt),
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		InstanceID:                uuid.NullUUID{UUID: instanceID, Valid: true},
		ApiKeyID:                  rotatedKey.ID,
	}))
	require.NoError(t, litellmrepo.New(ti.conn).RecordLiteLLMInstanceHealth(ctx, litellmrepo.RecordLiteLLMInstanceHealthParams{
		GuardrailObservedAt:       conv.PtrToPGTimestamptz(nil),
		OtelObservedAt:            conv.PtrToPGTimestamptz(nil),
		ErrorObservedAt:           conv.ToPGTimestamptz(relapsedAt.Add(500 * time.Millisecond)),
		ErrorKind:                 "decode_failure",
		ReportedLitellmVersion:    "1.93.0",
		ReportedVersionObservedAt: conv.ToPGTimestamptz(relapsedAt.Add(500 * time.Millisecond)),
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		InstanceID:                uuid.NullUUID{UUID: instanceID, Valid: true},
		ApiKeyID:                  rotatedKey.ID,
	}))
	listedAfterStaleError, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	finalDiagnostics := instanceByName(t, listedAfterStaleError, "with-traffic").Diagnostics
	require.Equal(t, gen.LiteLLMInstanceHealthStatus("success"), finalDiagnostics.Status)
	require.Equal(t, "1.95.0", *finalDiagnostics.ReportedLitellmVersion)
}

func liteLLMTrafficLog(t *testing.T, projectID, instanceID, apiKeyID uuid.UUID, observed time.Time, callID, email, userID, eventURN string) telemetryrepo.InsertTelemetryLogParams {
	t.Helper()
	attributes := map[string]any{
		"gram": map[string]any{
			"api_key": map[string]any{"id": apiKeyID.String()},
			"event":   map[string]any{"urn": eventURN},
			"litellm": map[string]any{"call_id": callID, "instance_id": instanceID.String(), "user_email": email},
		},
		"user": map[string]any{"id": userID},
	}
	encoded, err := json.Marshal(attributes)
	require.NoError(t, err)
	return telemetryrepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         observed.UnixNano(),
		ObservedTimeUnixNano: observed.UnixNano(),
		SeverityText:         nil,
		Body:                 "",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(encoded),
		ResourceAttributes:   "{}",
		GramProjectID:        projectID.String(),
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              litellmOTLPResourceURN,
		ServiceName:          "litellm",
		ServiceVersion:       nil,
		GramChatID:           nil,
	}
}

func instanceByNameCollect(collect *assert.CollectT, result *gen.ListInstancesResult, name string) *gen.LiteLLMInstance {
	for _, instance := range result.Instances {
		if instance.Name == name {
			return instance
		}
	}
	assert.Fail(collect, "instance not found", name)
	return nil
}
