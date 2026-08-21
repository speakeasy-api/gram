package otel

import (
	"context"
	"errors"
	"fmt"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/stokens"
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

type enrichLogSpeakeasyTokens struct {
	codec *stokens.Codec
}

func newEnrichLogSpeakeasyTokens() *enrichLogSpeakeasyTokens {
	return &enrichLogSpeakeasyTokens{codec: stokens.NewCodec()}
}

func (*enrichLogSpeakeasyTokens) Name() string {
	return "enrich-speakeasy-tokens"
}

func (e *enrichLogSpeakeasyTokens) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	recordDialect := dialect.ForLog(record)
	_, input, err := recordDialect.InputContent(record)
	if err != nil {
		return nil, fmt.Errorf("get input content from log: %w", err)
	}
	_, output, err := recordDialect.OutputContent(record)
	if err != nil {
		return nil, fmt.Errorf("get output content from log: %w", err)
	}

	if len(input) == 0 && len(output) == 0 {
		return nil, nil
	}

	inputCount, err := e.codec.CountInput(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("count input speakeasy tokens: %w", err)
	}
	outputCount, err := e.codec.CountOutput(ctx, output)
	if err != nil {
		return nil, fmt.Errorf("count output speakeasy tokens: %w", err)
	}

	return []attribute.KeyValue{
		TokensCount(inputCount + outputCount),
		TokensCodec(e.codec.Name()),
	}, nil
}
