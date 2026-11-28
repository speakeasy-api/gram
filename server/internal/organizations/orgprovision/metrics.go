package orgprovision

import (
	"context"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/emaildomain"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// SourceUnknown labels a created organization whose row records no creation
// source, such as one first written by WorkOS organization sync. It is a
// metric label only and is never stored.
const SourceUnknown = "unknown"

const meterOrganizationsCreated = "gram.organizations.created"

// CreationRecorder counts organization rows as they are created. Every path
// that inserts into organization_metadata reports through it, after its
// transaction commits and only when the statement actually inserted the row.
// A nil *CreationRecorder records nothing.
type CreationRecorder struct {
	created metric.Int64Counter
}

// NewCreationRecorder builds a recorder on meterProvider. A nil provider
// records nothing.
func NewCreationRecorder(meterProvider metric.MeterProvider) *CreationRecorder {
	if meterProvider == nil {
		meterProvider = noop.NewMeterProvider()
	}
	created, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/organizations/orgprovision").Int64Counter(
		meterOrganizationsCreated,
		metric.WithDescription("Organizations created, by creation source, account type, and email domain class."),
		metric.WithUnit("{organization}"),
	)
	if err != nil {
		// Instrument creation only fails on an invalid name, which is a
		// constant here.
		return &CreationRecorder{created: nil}
	}
	return &CreationRecorder{created: created}
}

// RecordCreated counts org as created. creatorEmail is the email of the user
// the organization was created for, or "" when no user created it; only its
// domain class is recorded. Call it only after the transaction that inserted
// org has committed.
func (r *CreationRecorder) RecordCreated(ctx context.Context, org orgRepo.OrganizationMetadatum, creatorEmail string) {
	if r == nil || r.created == nil {
		return
	}
	r.created.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationCreatedSource(CreationSourceLabel(org)),
		attr.OrganizationCreatedAccountType(org.GramAccountType),
		attr.OrganizationCreatedEmailDomainClass(string(emaildomain.Classify(creatorEmail))),
	))
}

// CreationSourceLabel is org's creation source as the counter labels it:
// SourceUnknown when the row records none.
func CreationSourceLabel(org orgRepo.OrganizationMetadatum) string {
	if source := conv.FromPGTextOrEmpty[string](org.CreationSource); source != "" {
		return source
	}
	return SourceUnknown
}
