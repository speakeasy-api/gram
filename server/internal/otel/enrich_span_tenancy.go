package otel

import (
	"context"
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/attribute"
)

type enrichTenancy struct{}

func (e *enrichTenancy) Name() string {
	return "enrich-tenancy"
}

func (e *enrichTenancy) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	prov := span.GetProvenance()
	orgID := prov.GetOrganizationId()
	projectID := prov.GetProjectId()

	if orgID == "" {
		return nil, errors.New("missing organization ID in span provenance")
	}

	if projectID == "" {
		return nil, errors.New("missing project ID in span provenance")
	}

	attrs := []attribute.KeyValue{
		OrganizationID(orgID),
		ProjectID(projectID),
	}

	if slug := prov.GetOrganizationSlug(); slug != "" {
		attrs = append(attrs, OrganizationSlug(slug))
	}

	if slug := prov.GetProjectSlug(); slug != "" {
		attrs = append(attrs, ProjectSlug(slug))
	}

	if keyID := prov.GetApiKeyId(); keyID != "" {
		attrs = append(attrs, APIKeyID(keyID))
	}

	if keyName := prov.GetApiKeyName(); keyName != "" {
		attrs = append(attrs, APIKeyName(keyName))
	}

	return attrs, nil
}
