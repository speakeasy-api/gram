package otel

import (
	"context"
	"fmt"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/tiktoken-go/tokenizer"
	"go.opentelemetry.io/otel/attribute"
)

type enrichSpeakeasyTokens struct {
	codec tokenizer.Codec
}

func NewEnrichSpeakeasyTokens() *enrichSpeakeasyTokens {
	codec, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		panic(fmt.Errorf("get %s: %w", tokenizer.O200kBase, err))
	}
	return &enrichSpeakeasyTokens{codec: codec}
}

func (e *enrichSpeakeasyTokens) Name() string {
	return "enrich-speakeasy-tokens"
}

func (e *enrichSpeakeasyTokens) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	ex := dialect.ForSpan(span)
	_, c, err := ex.Content(span)
	if err != nil {
		return nil, fmt.Errorf("get content from span: %w", err)
	}

	if len(c) == 0 {
		return nil, nil
	}

	count := 0
	for _, v := range c {
		n, err := e.codec.Count(v)
		if err != nil {
			return nil, fmt.Errorf("count speakeasy tokens: %w", err)
		}
		count += n
	}

	return []attribute.KeyValue{
		TokensCount(count),
		TokensCodec(e.codec.GetName()),
	}, nil
}
