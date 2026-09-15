package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestGetOneAttrSkipsWrongValueTypeAndUsesKeyPriority(t *testing.T) {
	t.Parallel()

	wrongTypeKey := "preferred"
	wrongTypeValue := int64(42)
	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			(&otelv1.InboundSpan_KeyValue_builder{
				Key: &wrongTypeKey,
				Value: (&otelv1.InboundSpan_AnyValue_builder{
					IntValue: &wrongTypeValue,
				}).Build(),
			}).Build(),
			stringAttribute("fallback", "fallback-value"),
			stringAttribute("preferred", "preferred-value"),
		},
	}).Build()

	key, value := getOneAttr(span, "preferred", "fallback")
	require.Equal(t, "preferred", key)
	require.Equal(t, "preferred-value", value)
}

func stringAttribute(key, value string) *otelv1.InboundSpan_KeyValue {
	return (&otelv1.InboundSpan_KeyValue_builder{
		Key: &key,
		Value: (&otelv1.InboundSpan_AnyValue_builder{
			StringValue: &value,
		}).Build(),
	}).Build()
}

func textInputMessages(content string) genaiconv.InputMessages {
	return genaiconv.InputMessages{
		{
			Role: genaiconv.RoleUser,
			Parts: []genaiconv.Part{
				&genaiconv.TextPart{
					Type:    genaiconv.PartTypeText,
					Content: content,
				},
			},
			Name: nil,
		},
	}
}

func textOutputMessages(content string) genaiconv.OutputMessages {
	return genaiconv.OutputMessages{
		{
			Role: genaiconv.RoleAssistant,
			Parts: []genaiconv.Part{
				&genaiconv.TextPart{
					Type:    genaiconv.PartTypeText,
					Content: content,
				},
			},
			Name:         nil,
			FinishReason: genaiconv.FinishReasonStop,
		},
	}
}

func structuredMessagesAttribute(key, role, content, finishReason string) *otelv1.InboundSpan_KeyValue {
	messageValues := []*otelv1.InboundSpan_KeyValue{
		stringAttribute("role", role),
		(&otelv1.InboundSpan_KeyValue_builder{
			Key: new("parts"),
			Value: (&otelv1.InboundSpan_AnyValue_builder{
				ArrayValue: (&otelv1.InboundSpan_ArrayValue_builder{
					Values: []*otelv1.InboundSpan_AnyValue{
						(&otelv1.InboundSpan_AnyValue_builder{
							KvlistValue: (&otelv1.InboundSpan_KeyValueList_builder{
								Values: []*otelv1.InboundSpan_KeyValue{
									stringAttribute("type", "text"),
									stringAttribute("content", content),
								},
							}).Build(),
						}).Build(),
					},
				}).Build(),
			}).Build(),
		}).Build(),
	}
	if finishReason != "" {
		messageValues = append(messageValues, stringAttribute("finish_reason", finishReason))
	}

	return (&otelv1.InboundSpan_KeyValue_builder{
		Key: &key,
		Value: (&otelv1.InboundSpan_AnyValue_builder{
			ArrayValue: (&otelv1.InboundSpan_ArrayValue_builder{
				Values: []*otelv1.InboundSpan_AnyValue{
					(&otelv1.InboundSpan_AnyValue_builder{
						KvlistValue: (&otelv1.InboundSpan_KeyValueList_builder{
							Values: messageValues,
						}).Build(),
					}).Build(),
				},
			}).Build(),
		}).Build(),
	}).Build()
}
