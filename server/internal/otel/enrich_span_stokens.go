package otel

import (
	"context"
	"fmt"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/stokens"
	"go.opentelemetry.io/otel/attribute"
)

type enrichSpeakeasyTokens struct {
	codec *stokens.Codec
}

func NewEnrichSpeakeasyTokens() *enrichSpeakeasyTokens {
	return &enrichSpeakeasyTokens{codec: stokens.NewCodec()}
}

func (e *enrichSpeakeasyTokens) Name() string {
	return "enrich-speakeasy-tokens"
}

func (e *enrichSpeakeasyTokens) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	ex := dialect.ForSpan(span)
	_, input, err := ex.InputContent(span)
	if err != nil {
		return nil, fmt.Errorf("get input content from span: %w", err)
	}
	_, output, err := ex.OutputContent(span)
	if err != nil {
		return nil, fmt.Errorf("get output content from span: %w", err)
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
	count := inputCount + outputCount

	return []attribute.KeyValue{
		TokensCount(count),
		TokensCodec(e.codec.Name()),
	}, nil
}
