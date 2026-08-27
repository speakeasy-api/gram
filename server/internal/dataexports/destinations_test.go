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
	created, err := ti.service.CreateOtelDestination(ctx, &gen.CreateOtelDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		EndpointURL:      "https://collector.example.test/otlp",
		SensitiveData:    "include",
		Headers: []*gen.OtelDestinationHeaderInput{{
			Name:  "Authorization",
			Value: &secret,
		}}})
	require.NoError(t, err)
	require.Equal(t, "include", created.SensitiveData)
	require.Equal(t, []*gen.OtelDestinationHeader{{Name: "Authorization", HasValue: true}}, created.Headers)

	listed, err := ti.service.ListOtelDestinations(ctx, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Destinations, 1)
	require.Equal(t, created, listed.Destinations[0])

	updated, err := ti.service.UpdateOtelDestination(ctx, &gen.UpdateOtelDestinationPayload{SessionToken: nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		EndpointURL:      "https://collector.example.test/new-base",
		SensitiveData:    "exclude",
		Headers: []*gen.OtelDestinationHeaderInput{{
			Name:  "authorization",
			Value: nil,
		}}})
	require.NoError(t, err)
	require.Equal(t, "exclude", updated.SensitiveData)
	require.Equal(t, []*gen.OtelDestinationHeader{{Name: "authorization", HasValue: true}}, updated.Headers)

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
	require.Equal(t, map[string]string{"authorization": secret}, storedHeaders)

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOtelDestinationUpdate)
	require.NoError(t, err)
	require.NotContains(t, string(auditRecord.BeforeSnapshot), secret)
	require.NotContains(t, string(auditRecord.AfterSnapshot), secret)
	beforeSnapshot, err := audittest.DecodeAuditData(auditRecord.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(auditRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, []any{"Authorization"}, beforeSnapshot["header_names"])
	require.Equal(t, []any{"authorization"}, afterSnapshot["header_names"])
	require.Equal(t, "include", beforeSnapshot["sensitive_data"])
	require.Equal(t, "exclude", afterSnapshot["sensitive_data"])

	require.NoError(t, ti.service.DeleteOtelDestination(ctx, &gen.DeleteOtelDestinationPayload{ID: created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil}))
	listed, err = ti.service.ListOtelDestinations(ctx, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Destinations)

	for _, action := range []audit.Action{audit.ActionOtelDestinationCreate, audit.ActionOtelDestinationUpdate, audit.ActionOtelDestinationDelete} {
		count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}
}

func TestDestinationRejectsUserinfoAndFragments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateOtelDestination(ctx, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://user:password@collector.example.test", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateOtelDestination(ctx, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://collector.example.test/otlp#fragment", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateOtelDestination(ctx, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://collector.example.test/otlp#", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestDestinationDeletionConflictsWhileRouteReferencesIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createOtelDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	_, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "otel_logs", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)

	err = ti.service.DeleteOtelDestination(ctx, &gen.DeleteOtelDestinationPayload{ID: destination.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	listed, err := ti.service.ListOtelDestinations(ctx, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Destinations, 1)
}

func TestDestinationCreateRollsBackWhenAuditInsertFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionOtelDestinationCreate))

	_, err := ti.service.CreateOtelDestination(ctx, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://collector.example.test", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
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
	_, err := ti.service.CreateOtelDestination(ctx, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://collector.example.test", SensitiveData: "redact", Headers: []*gen.OtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeInvalid)
}
