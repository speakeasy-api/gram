package dataexports_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDestinationCRUDPreservesWriteOnlyHeadersAndAuditsSafeSnapshots(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	secret := "initial-secret"
	secretToClear := "second-secret"
	created, err := ti.service.CreateDestination(ctx, &gen.CreateDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "Primary collector",
		DestinationType:  "otel",
		SensitiveData:    "include",
		Otel: &gen.CreateOtelDestinationInput{
			EndpointURL: "https://collector.example.test/otlp",
			Headers: []*gen.CreateOtelDestinationHeaderInput{
				{Name: "Authorization", Value: secret},
				{Name: "X-API-Key", Value: secretToClear},
			}}})
	require.NoError(t, err)
	require.Equal(t, "Primary collector", created.Name)
	require.Equal(t, "include", created.SensitiveData)
	require.Equal(t, "otel", created.DestinationType)
	require.NotNil(t, created.Otel)
	require.Equal(t, []*gen.OtelDestinationHeader{
		{Name: "Authorization", HasValue: true},
		{Name: "X-API-Key", HasValue: true},
	}, created.Otel.Headers)

	listed, err := ti.service.ListDestinations(ctx, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Destinations, 1)
	require.Equal(t, created, listed.Destinations[0])

	cleared := ""
	updated, err := ti.service.UpdateDestination(ctx, &gen.UpdateDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Name:             "Renamed collector",
		DestinationType:  "otel",
		SensitiveData:    "exclude",
		Otel: &gen.UpdateOtelDestinationInput{
			EndpointURL: "https://collector.example.test/new-base",
			Headers: []*gen.UpdateOtelDestinationHeaderInput{
				{Name: "authorization", Value: nil},
				{Name: "x-api-key", Value: &cleared},
			}}})
	require.NoError(t, err)
	require.Equal(t, "Renamed collector", updated.Name)
	require.Equal(t, "exclude", updated.SensitiveData)
	require.Equal(t, []*gen.OtelDestinationHeader{
		{Name: "authorization", HasValue: true},
		{Name: "x-api-key", HasValue: false},
	}, updated.Otel.Headers)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := repo.New(ti.conn).ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, created.ID, row.ID.String())
	require.True(t, row.HeadersEncrypted.Valid)
	require.NotContains(t, row.HeadersEncrypted.String, secret)
	plaintext, err := ti.enc.Decrypt(row.HeadersEncrypted.String)
	require.NoError(t, err)
	var storedHeaders map[string]string
	require.NoError(t, json.Unmarshal([]byte(plaintext), &storedHeaders))
	require.Equal(t, map[string]string{"authorization": secret, "x-api-key": ""}, storedHeaders)

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOtelDestinationUpdate)
	require.NoError(t, err)
	require.NotContains(t, string(auditRecord.BeforeSnapshot), secret)
	require.NotContains(t, string(auditRecord.AfterSnapshot), secret)
	require.NotContains(t, string(auditRecord.BeforeSnapshot), secretToClear)
	require.NotContains(t, string(auditRecord.AfterSnapshot), secretToClear)
	beforeSnapshot, err := audittest.DecodeAuditData(auditRecord.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(auditRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Primary collector", beforeSnapshot["name"])
	require.Equal(t, "Renamed collector", afterSnapshot["name"])
	require.Equal(t, []any{
		map[string]any{"name": "Authorization", "has_value": true},
		map[string]any{"name": "X-API-Key", "has_value": true},
	}, beforeSnapshot["headers"])
	require.Equal(t, []any{
		map[string]any{"name": "authorization", "has_value": true},
		map[string]any{"name": "x-api-key", "has_value": false},
	}, afterSnapshot["headers"])
	require.Equal(t, "include", beforeSnapshot["sensitive_data"])
	require.Equal(t, "exclude", afterSnapshot["sensitive_data"])

	require.NoError(t, ti.service.DeleteDestination(ctx, &gen.DeleteDestinationPayload{ID: created.ID,
		DestinationType:  "otel",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil}))
	listed, err = ti.service.ListDestinations(ctx, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Destinations)

	for _, action := range []audit.Action{audit.ActionOtelDestinationCreate, audit.ActionOtelDestinationUpdate, audit.ActionOtelDestinationDelete} {
		count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}
}

func TestDestinationUpdateReusesUnchangedEncryptedHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateDestination(ctx, &gen.CreateDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             "Primary collector",
		DestinationType:  "otel",
		SensitiveData:    "exclude",
		Otel: &gen.CreateOtelDestinationInput{
			EndpointURL: "https://collector.example.test/otlp",
			Headers: []*gen.CreateOtelDestinationHeaderInput{
				{Name: "Authorization", Value: "initial-secret"},
			}}})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := repo.New(ti.conn).ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	beforeCiphertext := rows[0].HeadersEncrypted

	_, err = ti.service.UpdateDestination(ctx, &gen.UpdateDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		Name:             "Renamed collector",
		DestinationType:  "otel",
		SensitiveData:    created.SensitiveData,
		Otel: &gen.UpdateOtelDestinationInput{
			EndpointURL: created.Otel.EndpointURL,
			Headers:     []*gen.UpdateOtelDestinationHeaderInput{{Name: "Authorization", Value: nil}},
		},
	})
	require.NoError(t, err)

	rows, err = repo.New(ti.conn).ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, beforeCiphertext, rows[0].HeadersEncrypted)
}

func TestDestinationRejectsUserinfoAndFragments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateDestination(ctx, otelDestinationPayload("https://user:password@collector.example.test"))
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateDestination(ctx, otelDestinationPayload("https://collector.example.test/otlp#fragment"))
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateDestination(ctx, otelDestinationPayload("https://collector.example.test/otlp#"))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationRejectsMissingHostname(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateDestination(ctx, otelDestinationPayload("https://:4318"))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationRejectsQueries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateDestination(ctx, otelDestinationPayload("https://collector.example.test/otlp?token=secret"))
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateDestination(ctx, otelDestinationPayload("https://collector.example.test/otlp?"))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationRejectsInvalidHeaderValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := otelDestinationPayload("https://collector.example.test")
	payload.Otel.Headers = []*gen.CreateOtelDestinationHeaderInput{{Name: "Authorization", Value: "Bearer safe\r\nX-Injected: true"}}
	_, err := ti.service.CreateDestination(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationAllowsHTTP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateDestination(ctx, otelDestinationPayload("http://collector.example.test:4318"))
	require.NoError(t, err)
	require.Equal(t, "http://collector.example.test:4318", created.Otel.EndpointURL)
}

func TestDestinationDeletionConflictsWhileRouteReferencesIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	_, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)

	err = ti.service.DeleteDestination(ctx, &gen.DeleteDestinationPayload{ID: destination.ID, DestinationType: "otel", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	listed, err := ti.service.ListDestinations(ctx, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Destinations, 1)
}

func TestDestinationCreateRollsBackWhenAuditInsertFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionOtelDestinationCreate))

	_, err := ti.service.CreateDestination(ctx, otelDestinationPayload("https://collector.example.test"))
	requireOopsCode(t, err, oops.CodeUnexpected)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := repo.New(ti.conn).ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestDestinationRejectsUnknownSensitiveDataPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := otelDestinationPayload("https://collector.example.test")
	payload.SensitiveData = "redact"
	_, err := ti.service.CreateDestination(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationRejectsBlankName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := otelDestinationPayload("https://collector.example.test")
	payload.Name = "  "
	_, err := ti.service.CreateDestination(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationRejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := otelDestinationPayload("https://collector.example.test")
	payload.DestinationType = "siem"
	_, err := ti.service.CreateDestination(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestOtelDestinationRequiresOtelConfiguration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := otelDestinationPayload("https://collector.example.test")
	payload.Otel = nil
	_, err := ti.service.CreateDestination(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func otelDestinationPayload(endpointURL string) *gen.CreateDestinationPayload {
	return &gen.CreateDestinationPayload{
		Name:            "Test destination",
		DestinationType: "otel",
		SensitiveData:   "exclude",
		Otel: &gen.CreateOtelDestinationInput{
			EndpointURL: endpointURL,
			Headers:     []*gen.CreateOtelDestinationHeaderInput{},
		},
	}
}
