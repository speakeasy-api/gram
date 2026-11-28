package admin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type orgCreatedLabels struct {
	source           string
	accountType      string
	emailDomainClass string
}

func newOrgCreationReader(t *testing.T) (*orgprovision.CreationRecorder, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return orgprovision.NewCreationRecorder(provider), reader
}

func collectOrgsCreated(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) map[orgCreatedLabels]int64 {
	t.Helper()

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &metrics))
	counts := map[orgCreatedLabels]int64{}
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.organizations.created" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				source, _ := point.Attributes.Value(attr.OrganizationCreatedSourceKey)
				accountType, _ := point.Attributes.Value(attr.OrganizationCreatedAccountTypeKey)
				class, _ := point.Attributes.Value(attr.OrganizationCreatedEmailDomainClassKey)
				counts[orgCreatedLabels{source: source.AsString(), accountType: accountType.AsString(), emailDomainClass: class.AsString()}] += point.Value
			}
		}
	}
	return counts
}

func TestCreateOrganization_CountsCreatedOrganization(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZCOUNTADMINCREATE"
	ctx, svc, _ := newTestAdminServiceWithWorkOS(t, newFakeWorkOS(workosOrgID))
	recorder, reader := newOrgCreationReader(t)
	svc.SetOrganizationCreationRecorder(recorder)

	_, err := svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "count.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, map[orgCreatedLabels]int64{
		{source: orgprovision.SourcePlatformAdmin, accountType: "free", emailDomainClass: "unknown"}: 1,
	}, collectOrgsCreated(t, ctx, reader))
}

// When WorkOS sync inserts the row first, the sync counts it with no known
// source, and the admin create that then updates the row counts nothing.
func TestCreateOrganization_UpdatingASyncedRowIsNotCounted(t *testing.T) {
	t.Parallel()

	const workosOrgID = "org_01HZCOUNTHOOKTHENADMIN"
	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, newFakeWorkOS(workosOrgID))
	recorder, reader := newOrgCreationReader(t)
	svc.SetOrganizationCreationRecorder(recorder)

	stub := workos.NewStubClient()
	stub.SetEventPages([][]events.Event{{organizationEvent("event_01HZCOUNT", "organization.created", workosOrgID, "Count Co", "")}})
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	activity.SetOrganizationCreationRecorder(recorder)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID, SinceEventID: nil})
	require.NoError(t, err)

	require.Equal(t, map[orgCreatedLabels]int64{
		{source: orgprovision.SourceUnknown, accountType: "free", emailDomainClass: "unknown"}: 1,
	}, collectOrgsCreated(t, ctx, reader))

	_, err = svc.CreateOrganization(ctx, &gen.CreateOrganizationPayload{URL: "count-race.example.com", OwnershipConfirmed: true, AdminSessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, map[orgCreatedLabels]int64{
		{source: orgprovision.SourceUnknown, accountType: "free", emailDomainClass: "unknown"}: 1,
	}, collectOrgsCreated(t, ctx, reader), "an upsert that updated an existing row must not count as a creation")
}
