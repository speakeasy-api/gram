package gcp

import (
	pubsubv1beta1 "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/pubsub/v1beta1"
)

// pubSubValuesDocument is the top-level Helm values document emitted by the
// generator. The deployment chart's pubsub template ranges over the topology
// under the `pubsub` key to render Config Connector resources.
type pubSubValuesDocument struct {
	PubSub pubSubValues `json:"pubsub"`
}

// pubSubValues is the Pub/Sub topology projected as Helm values. Per-resource
// deployment metadata (project, namespace, deletion/prune policy) is stamped by
// the chart template, not the generator.
type pubSubValues struct {
	Enabled       bool                      `json:"enabled"`
	APIs          []string                  `json:"apis"`
	Topics        []pubSubTopicValue        `json:"topics"`
	Subscriptions []pubSubSubscriptionValue `json:"subscriptions"`
	Schemas       []pubSubSchemaValue       `json:"schemas"`
}

// pubSubTopicValue carries a topic's name, labels, and KCC spec. The spec reuses
// the Config Connector type so generated field names match the CRD exactly.
type pubSubTopicValue struct {
	Name   string                        `json:"name"`
	Labels map[string]string             `json:"labels,omitempty"`
	Spec   pubsubv1beta1.PubSubTopicSpec `json:"spec"`
}

// pubSubSubscriptionValue carries a subscription's name, labels, and KCC spec.
type pubSubSubscriptionValue struct {
	Name   string                               `json:"name"`
	Labels map[string]string                    `json:"labels,omitempty"`
	Spec   pubsubv1beta1.PubSubSubscriptionSpec `json:"spec"`
}

// pubSubSchemaValue carries a schema's name, labels, and a minimal spec. Unlike
// topics and subscriptions, the spec is not the full KCC PubSubSchemaSpec: that
// type requires a projectRef, which (like all per-resource deployment metadata)
// is stamped by the chart template, not the generator. We emit only the
// generator-owned fields.
type pubSubSchemaValue struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Spec   pubSubSchemaSpec  `json:"spec"`
}

// pubSubSchemaSpec is the generator-owned subset of the Config Connector
// PubSubSchemaSpec: the schema type and the inlined protobuf definition.
type pubSubSchemaSpec struct {
	Type       string `json:"type"`
	Definition string `json:"definition"`
}
