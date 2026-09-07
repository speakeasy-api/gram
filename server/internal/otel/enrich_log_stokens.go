package otel

import (
	"context"
	"fmt"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/stokens"
	"go.opentelemetry.io/otel/attribute"
)

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
