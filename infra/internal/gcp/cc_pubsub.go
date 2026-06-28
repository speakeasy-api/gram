package gcp

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"math"
	"slices"

	kccv1alpha1 "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/k8s/v1alpha1"
	pubsubv1beta1 "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/pubsub/v1beta1"
	"github.com/ettle/strcase"
	"github.com/speakeasy-api/gram/infra/internal/attr"
)

// protoMessageLabel is the metadata label key carrying the fully qualified
// protobuf message name a topic or subscription was generated from.
const protoMessageLabel = "proto_message"

// topicMessageLabel is the metadata label key carrying the fully qualified
// protobuf message name of the topic a subscription consumes.
const topicMessageLabel = "topic_proto_message"

// schemaEncodingBinary is the Config Connector TopicSchemaSettings `encoding`
// value for messages validated as wire-format protobuf. The runtime publisher
// marshals proto messages to binary, so attached schemas validate against the
// binary encoding rather than JSON.
const schemaEncodingBinary = "BINARY"

// buildPubSubValues projects the discovered topology into a stable, sorted Helm
// values document. Topics and subscriptions are sorted by name so the generated
// file diffs cleanly across runs.
//
// A topic gets its generated schema attached (via schemaSettings) when one was
// derived from the same proto message. A topic that sets an explicit `name`
// option is left unattached — its ID may point at a shared, externally-owned
// topic, so binding a per-message schema to it could collide with other
// producers — and the skip is logged.
func buildPubSubValues(ctx context.Context, logger *slog.Logger, topics []DesiredTopic, subs []DesiredSubscription, schemas []DesiredSchema) pubSubValuesDocument {
	sortedTopics := slices.Clone(topics)
	slices.SortFunc(sortedTopics, func(a, b DesiredTopic) int {
		return cmp.Compare(a.Name, b.Name)
	})

	sortedSubs := slices.Clone(subs)
	slices.SortFunc(sortedSubs, func(a, b DesiredSubscription) int {
		return cmp.Compare(a.Name, b.Name)
	})

	sortedSchemas := slices.Clone(schemas)
	slices.SortFunc(sortedSchemas, func(a, b DesiredSchema) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// Index schemas by their source proto message so a topic can find the schema
	// derived from the same message regardless of the topic's resolved ID.
	schemaByProtoMessage := make(map[string]DesiredSchema, len(sortedSchemas))
	for _, schema := range sortedSchemas {
		schemaByProtoMessage[schema.ProtoMessage] = schema
	}

	topicValues := make([]pubSubTopicValue, 0, len(sortedTopics))
	for _, topic := range sortedTopics {
		spec := topicSpec(topic)

		if schema, ok := schemaByProtoMessage[topic.ProtoMessage]; ok {
			if topic.NameOverridden {
				logger.InfoContext(ctx,
					"skipping pubsub schema attachment for topic with explicit name option",
					attr.SlogGCPTopicQualifiedName(topic.Name),
					attr.SlogTopicProtoName(topic.ProtoMessage),
				)
			} else {
				spec.SchemaSettings = &pubsubv1beta1.TopicSchemaSettings{
					Encoding:  new(schemaEncodingBinary),
					SchemaRef: kccv1alpha1.ResourceRef{Name: schema.Name},
				}
			}
		}

		topicValues = append(topicValues, pubSubTopicValue{
			Name:   topic.Name,
			Labels: labelsWithProtoMessage(topic.Labels, topic.ProtoMessage),
			Spec:   spec,
		})
	}

	subValues := make([]pubSubSubscriptionValue, 0, len(sortedSubs))
	for _, sub := range sortedSubs {
		labels := labelsWithProtoMessage(sub.Labels, sub.ProtoMessage)
		if sub.TopicMessage != "" {
			labels[topicMessageLabel] = sanitizeLabelValue(sub.TopicMessage)
		}
		subValues = append(subValues, pubSubSubscriptionValue{
			Name:   sub.Name,
			Labels: labels,
			Spec:   subscriptionSpec(sub),
		})
	}

	schemaValues := make([]pubSubSchemaValue, 0, len(sortedSchemas))
	for _, schema := range sortedSchemas {
		schemaValues = append(schemaValues, pubSubSchemaValue{
			Name:   schema.Name,
			Labels: labelsWithProtoMessage(schema.Labels, schema.ProtoMessage),
			Spec: pubSubSchemaSpec{
				Type:       schemaTypeProtocolBuffer,
				Definition: schema.Definition,
			},
		})
	}

	return pubSubValuesDocument{
		PubSub: pubSubValues{
			Enabled:       true,
			APIs:          []string{pubsubAPI},
			Topics:        topicValues,
			Subscriptions: subValues,
			Schemas:       schemaValues,
		},
	}
}

// labelsWithProtoMessage returns a copy of labels with the proto message label
// added when a fully qualified message name is available, leaving the input map
// untouched.
func labelsWithProtoMessage(labels map[string]string, protoMessage string) map[string]string {
	out := maps.Clone(labels)
	if out == nil {
		out = map[string]string{}
	}
	if protoMessage != "" {
		out[protoMessageLabel] = sanitizeLabelValue(protoMessage)
	}
	return out
}

// sanitizeLabelValue converts a fully qualified protobuf message name into a
// valid GCP label value. Label values must match [\p{Ll}\p{Lo}\p{N}_-]{0,63},
// so the dotted, mixed-case proto full name (e.g. "gram.ping.v2.Message") is
// kebab-cased to "gram-ping-v2-message" — the same transform used to derive
// topic and subscription IDs, keeping resources traceable to their declaration.
func sanitizeLabelValue(protoMessage string) string {
	return strcase.ToKebab(protoMessage)
}

func topicSpec(desired DesiredTopic) pubsubv1beta1.PubSubTopicSpec {
	spec := pubsubv1beta1.PubSubTopicSpec{
		KmsKeyRef:                nil,
		MessageRetentionDuration: nil,
		MessageStoragePolicy:     nil,
		ResourceID:               nil,
		SchemaSettings:           nil,
	}

	if desired.Retention > 0 {
		spec.MessageRetentionDuration = new(durationToGCPString(desired.Retention))
	}

	return spec
}

func subscriptionSpec(desired DesiredSubscription) pubsubv1beta1.PubSubSubscriptionSpec {
	spec := pubsubv1beta1.PubSubSubscriptionSpec{
		TopicRef:                  kccv1alpha1.ResourceRef{Name: desired.Topic},
		RetainAckedMessages:       new(desired.RetainAckedMessages),
		AckDeadlineSeconds:        nil,
		BigqueryConfig:            nil,
		CloudStorageConfig:        nil,
		DeadLetterPolicy:          nil,
		EnableExactlyOnceDelivery: nil,
		EnableMessageOrdering:     nil,
		ExpirationPolicy:          nil,
		Filter:                    nil,
		MessageRetentionDuration:  nil,
		PushConfig:                nil,
		ResourceID:                nil,
		RetryPolicy:               nil,
	}

	// Round to the nearest second rather than truncating: flooring would
	// silently shrink configured deadlines (e.g. 1.5s → 1s). This matches the
	// emulator broker's half-up rounding so the same config behaves identically
	// locally and in GCP. Guard on the rounded value so a sub-second duration
	// that rounds to 0 is left unset (GCP applies its default) instead of
	// emitting an invalid 0.
	if secs := int64(math.Round(desired.AckDeadline.Seconds())); secs > 0 {
		spec.AckDeadlineSeconds = new(secs)
	}

	if desired.Retention > 0 {
		spec.MessageRetentionDuration = new(durationToGCPString(desired.Retention))
	}

	if desired.Filter != "" {
		spec.Filter = new(desired.Filter)
	}

	if desired.ExpirationTTL > 0 {
		spec.ExpirationPolicy = &pubsubv1beta1.SubscriptionExpirationPolicy{
			Ttl: durationToGCPString(desired.ExpirationTTL),
		}
	}

	if desired.RetryPolicy != nil {
		spec.RetryPolicy = &pubsubv1beta1.SubscriptionRetryPolicy{
			MinimumBackoff: new(durationToGCPString(desired.RetryPolicy.MinimumBackoff)),
			MaximumBackoff: new(durationToGCPString(desired.RetryPolicy.MaximumBackoff)),
		}
	}

	if desired.DeadLetterTopic != "" {
		spec.DeadLetterPolicy = &pubsubv1beta1.SubscriptionDeadLetterPolicy{
			DeadLetterTopicRef:  &kccv1alpha1.ResourceRef{Name: desired.DeadLetterTopic},
			MaxDeliveryAttempts: new(int64(desired.MaxDeliveryAttempts)),
		}
	}

	return spec
}
