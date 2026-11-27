package otel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func createOrgFixture(t *testing.T, db testrepo.DBTX, id string, disabled bool) {
	t.Helper()
	params := testrepo.CreateOrganizationMetadataFixtureParams{
		ID: id, Name: id, Slug: id, GramAccountType: "free",
		WorkosID: pgtype.Text{}, Whitelisted: false,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(14 * 24 * time.Hour)),
		DisabledAt: pgtype.Timestamptz{}, CreatedAt: pgtype.Timestamptz{},
	}
	if disabled {
		params.DisabledAt = conv.ToPGTimestamptz(time.Now())
	}
	require.NoError(t, testrepo.New(db).CreateOrganizationMetadataFixture(t.Context(), params))
}

func TestOrgDisabledCheckerCachesAndExpires(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	ctx := t.Context()
	createOrgFixture(t, db, "org_checker", false)

	now := time.Now()
	checker := newOrgDisabledChecker(db)
	checker.now = func() time.Time { return now }

	disabled, err := checker.Disabled(ctx, "org_checker")
	require.NoError(t, err)
	require.False(t, disabled)

	_, err = adminrepo.New(db).AdminDisableOrganization(ctx, "org_checker")
	require.NoError(t, err)
	disabled, err = checker.Disabled(ctx, "org_checker")
	require.NoError(t, err)
	require.False(t, disabled, "a cached answer holds until it expires")

	now = now.Add(orgDisabledTTL + time.Second)
	disabled, err = checker.Disabled(ctx, "org_checker")
	require.NoError(t, err)
	require.True(t, disabled)

	disabled, err = checker.Disabled(ctx, "org_unknown")
	require.NoError(t, err)
	require.False(t, disabled, "an unknown organization is not treated as disabled")
}

const disabledOrgID = "org_disabled_transform"

func TestTransformHandlersDropDisabledOrganizations(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	ctx := t.Context()
	createOrgFixture(t, db, disabledOrgID, true)

	logPub := gcp.NewMockPublisher[*otelv1.LogRecord]()
	logs := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), logPub, db, cache.NoopCache)
	err := logs.Handle(ctx, (&otelv1.InboundLogRecord_builder{
		RecordId: new("record-id"),
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			OrganizationId: new(disabledOrgID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
	}).Build(), gcp.MessageMetadata{})
	require.NoError(t, err)
	logPub.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)

	spanPub := gcp.NewMockPublisher[*otelv1.Span]()
	spans := NewSpanTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), spanPub, db, cache.NoopCache)
	err = spans.Handle(ctx, (&otelv1.InboundSpan_builder{
		Provenance: (&otelv1.InboundSpan_Provenance_builder{
			OrganizationId: new(disabledOrgID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
	}).Build(), gcp.MessageMetadata{})
	require.NoError(t, err)
	spanPub.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)

	metricPub := gcp.NewMockPublisher[*otelv1.Metric]()
	metrics := NewMetricTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), metricPub, db)
	err = metrics.Handle(ctx, (&otelv1.InboundMetric_builder{
		Name: new("requests"),
		Provenance: (&otelv1.InboundMetric_Provenance_builder{
			OrganizationId: new(disabledOrgID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
	}).Build(), gcp.MessageMetadata{})
	require.NoError(t, err)
	metricPub.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}

type failingDB struct{}

func (failingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("database unavailable")
}

func (failingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("database unavailable")
}

func (failingDB) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{} }

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("database unavailable") }

func TestLogTransformHandlerRedeliversWhenOrganizationLookupFails(t *testing.T) {
	t.Parallel()

	publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
	handler := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, failingDB{}, cache.NoopCache)
	err := handler.Handle(t.Context(), (&otelv1.InboundLogRecord_builder{
		RecordId: new("record-id"),
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			OrganizationId: new(disabledOrgID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
	}).Build(), gcp.MessageMetadata{})
	require.Error(t, err, "a failed lookup must nack so the record is redelivered")
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}
