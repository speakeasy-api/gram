package otel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestOrgDisabledCheckerCachesAndExpires(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, "INSERT INTO organization_metadata (id, name, slug) VALUES ('org_checker', 'Checker', 'checker')")
	require.NoError(t, err)

	now := time.Now()
	checker := newOrgDisabledChecker(db)
	checker.now = func() time.Time { return now }

	disabled, err := checker.Disabled(ctx, "org_checker")
	require.NoError(t, err)
	require.False(t, disabled)

	_, err = db.Exec(ctx, "UPDATE organization_metadata SET disabled_at = clock_timestamp() WHERE id = 'org_checker'")
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
	_, err := db.Exec(ctx, "INSERT INTO organization_metadata (id, name, slug, disabled_at) VALUES ($1, 'Disabled', 'disabled', clock_timestamp())", disabledOrgID)
	require.NoError(t, err)

	logPub := gcp.NewMockPublisher[*otelv1.LogRecord]()
	logs := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), logPub, db, cache.NoopCache)
	err = logs.Handle(ctx, (&otelv1.InboundLogRecord_builder{
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
