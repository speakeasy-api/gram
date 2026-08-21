package otel

import (
	"context"
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/attribute"
)

type enrichLogTenancy struct{}

func (*enrichLogTenancy) Name() string {
	return "enrich-tenancy"
}

func (*enrichLogTenancy) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	provenance := record.GetProvenance()
	organizationID := provenance.GetOrganizationId()
	projectID := provenance.GetProjectId()

	if organizationID == "" {
		return nil, errors.New("missing organization ID in log provenance")
	}
	if projectID == "" {
		return nil, errors.New("missing project ID in log provenance")
	}

	return []attribute.KeyValue{
		OrganizationID(organizationID),
		ProjectID(projectID),
	}, nil
}
