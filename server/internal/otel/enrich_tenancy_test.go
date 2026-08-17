package otel

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestEnrichTenancyRequiresOrganizationID(t *testing.T) {
	t.Parallel()

	projectID := "project-id"
	span := (&otelv1.InboundSpan_builder{
		Provenance: (&otelv1.InboundSpan_Provenance_builder{ProjectId: &projectID}).Build(),
	}).Build()

	attrs, err := (&enrichTenancy{}).Enrich(t.Context(), span)

	require.EqualError(t, err, "missing organization ID in span provenance")
	require.Nil(t, attrs)
}

func TestEnrichTenancyRequiresProjectID(t *testing.T) {
	t.Parallel()

	organizationID := "organization-id"
	span := (&otelv1.InboundSpan_builder{
		Provenance: (&otelv1.InboundSpan_Provenance_builder{OrganizationId: &organizationID}).Build(),
	}).Build()

	attrs, err := (&enrichTenancy{}).Enrich(t.Context(), span)

	require.EqualError(t, err, "missing project ID in span provenance")
	require.Nil(t, attrs)
}

func TestEnrichTenancyIncludesAvailableProvenance(t *testing.T) {
	t.Parallel()

	organizationID := "organization-id"
	organizationSlug := "organization-slug"
	projectID := "project-id"
	projectSlug := "project-slug"
	apiKeyID := "api-key-id"
	apiKeyName := "api-key-name"
	span := (&otelv1.InboundSpan_builder{
		Provenance: (&otelv1.InboundSpan_Provenance_builder{
			OrganizationId:   &organizationID,
			OrganizationSlug: &organizationSlug,
			ProjectId:        &projectID,
			ProjectSlug:      &projectSlug,
			ApiKeyId:         &apiKeyID,
			ApiKeyName:       &apiKeyName,
		}).Build(),
	}).Build()

	attrs, err := (&enrichTenancy{}).Enrich(t.Context(), span)

	require.NoError(t, err)
	require.Equal(t, []attribute.KeyValue{
		attribute.String("speakeasy.organization.id", organizationID),
		attribute.String("speakeasy.project.id", projectID),
		attribute.String("speakeasy.organization.slug", organizationSlug),
		attribute.String("speakeasy.project.slug", projectSlug),
		attribute.String("speakeasy.api_key.id", apiKeyID),
		attribute.String("speakeasy.api_key.name", apiKeyName),
	}, attrs)
}
