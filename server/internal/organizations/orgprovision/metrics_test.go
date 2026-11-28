package orgprovision_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestCreationRecorder_LabelsBySourceAndAccountType(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	recorder := orgprovision.NewCreationRecorder(provider)
	recorder.RecordCreated(ctx, orgRepo.OrganizationMetadatum{CreationSource: conv.ToPGText(orgprovision.SourceSignup), GramAccountType: "free"}, "jane@gmail.com")
	recorder.RecordCreated(ctx, orgRepo.OrganizationMetadatum{CreationSource: conv.ToPGText(orgprovision.SourceSignup), GramAccountType: "free"}, "jane@acme.com")
	recorder.RecordCreated(ctx, orgRepo.OrganizationMetadatum{CreationSource: conv.ToPGText(orgprovision.SourcePlatformAdmin), GramAccountType: "enterprise"}, "")
	recorder.RecordCreated(ctx, orgRepo.OrganizationMetadatum{CreationSource: pgtype.Text{String: "", Valid: false}, GramAccountType: "free"}, "jane@mailinator.com")

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &metrics))

	type labels struct{ source, accountType, emailDomainClass string }
	counts := map[labels]int64{}
	var unit string
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.organizations.created" {
				continue
			}
			unit = m.Unit
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.True(t, sum.IsMonotonic)
			for _, point := range sum.DataPoints {
				require.Equal(t, 3, point.Attributes.Len(), "the counter carries only low-cardinality labels")
				source, _ := point.Attributes.Value(attr.OrganizationCreatedSourceKey)
				accountType, _ := point.Attributes.Value(attr.OrganizationCreatedAccountTypeKey)
				class, _ := point.Attributes.Value(attr.OrganizationCreatedEmailDomainClassKey)
				counts[labels{source.AsString(), accountType.AsString(), class.AsString()}] += point.Value
			}
		}
	}

	require.Equal(t, "{organization}", unit)
	require.Equal(t, map[labels]int64{
		{orgprovision.SourceSignup, "free", "free_webmail"}:         1,
		{orgprovision.SourceSignup, "free", "business"}:             1,
		{orgprovision.SourcePlatformAdmin, "enterprise", "unknown"}: 1,
		{orgprovision.SourceUnknown, "free", "disposable"}:          1,
	}, counts)
}

func TestCreationRecorder_NilRecordsNothing(t *testing.T) {
	t.Parallel()

	var recorder *orgprovision.CreationRecorder
	recorder.RecordCreated(t.Context(), orgRepo.OrganizationMetadatum{GramAccountType: "free"}, "")
}
