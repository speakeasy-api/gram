package gcp

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

func fieldProto(name string, num int32, label descriptorpb.FieldDescriptorProto_Label, typ descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
	f := &descriptorpb.FieldDescriptorProto{
		Name:   new(name),
		Number: new(num),
		Label:  label.Enum(),
		Type:   typ.Enum(),
	}
	if typeName != "" {
		f.TypeName = new(typeName)
	}
	return f
}

// bqSchemaTestMessage builds a single Event message exercising every BigQuery
// type branch: scalars, repeated, enum, nested RECORD, and a map (REPEATED
// RECORD of key/value).
func bqSchemaTestMessage(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()

	const (
		opt = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		rep = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	)

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:    new("test/bq/v1/bq.proto"),
		Package: new("test.bq.v1"),
		Syntax:  new("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{
			{
				Name: new("Kind"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: new("KIND_UNSPECIFIED"), Number: new(int32(0))},
					{Name: new("KIND_A"), Number: new(int32(1))},
				},
			},
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: new("Nested"),
				Field: []*descriptorpb.FieldDescriptorProto{
					fieldProto("inner", 1, opt, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				},
			},
			{
				Name: new("Event"),
				Field: []*descriptorpb.FieldDescriptorProto{
					fieldProto("id", 1, opt, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
					fieldProto("count", 2, opt, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
					fieldProto("score", 3, opt, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, ""),
					fieldProto("active", 4, opt, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
					fieldProto("data", 5, opt, descriptorpb.FieldDescriptorProto_TYPE_BYTES, ""),
					fieldProto("tags", 6, rep, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
					fieldProto("kind", 7, opt, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".test.bq.v1.Kind"),
					fieldProto("nested", 8, opt, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".test.bq.v1.Nested"),
					fieldProto("attrs", 9, rep, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".test.bq.v1.Event.AttrsEntry"),
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: new("AttrsEntry"),
						Field: []*descriptorpb.FieldDescriptorProto{
							fieldProto("key", 1, opt, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
							fieldProto("value", 2, opt, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
						},
						Options: &descriptorpb.MessageOptions{MapEntry: new(true)},
					},
				},
			},
		},
	}

	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	files, err := protodesc.NewFiles(set)
	require.NoError(t, err)

	desc, err := files.FindDescriptorByName("test.bq.v1.Event")
	require.NoError(t, err)
	msg, ok := desc.(protoreflect.MessageDescriptor)
	require.True(t, ok)
	return msg
}

func TestBigqueryTableSchema(t *testing.T) {
	t.Parallel()

	schema, err := bigqueryTableSchema(bqSchemaTestMessage(t))
	require.NoError(t, err)

	// The schema is indented JSON so it renders as a readable multi-line YAML
	// block. Decoding it back and re-marshaling compactly keeps this assertion
	// resilient to indentation tweaks while still pinning the derived shape.
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, []byte(schema)))

	want := `[` +
		`{"name":"id","type":"STRING","mode":"NULLABLE"},` +
		`{"name":"count","type":"INTEGER","mode":"NULLABLE"},` +
		`{"name":"score","type":"FLOAT","mode":"NULLABLE"},` +
		`{"name":"active","type":"BOOLEAN","mode":"NULLABLE"},` +
		`{"name":"data","type":"BYTES","mode":"NULLABLE"},` +
		`{"name":"tags","type":"STRING","mode":"REPEATED"},` +
		`{"name":"kind","type":"STRING","mode":"NULLABLE"},` +
		`{"name":"nested","type":"RECORD","mode":"NULLABLE","fields":[{"name":"inner","type":"STRING","mode":"NULLABLE"}]},` +
		`{"name":"attrs","type":"RECORD","mode":"REPEATED","fields":[{"name":"key","type":"STRING","mode":"NULLABLE"},{"name":"value","type":"INTEGER","mode":"NULLABLE"}]}` +
		`]`

	require.Equal(t, want, compact.String())

	// The indented form must actually contain newlines for the YAML block scalar.
	require.Contains(t, schema, "\n")
}

// TestBigquerySchemaFields_OrderedByFieldNumber confirms columns are emitted in
// field-number order regardless of the order fields are declared in the proto.
func TestBigquerySchemaFields_OrderedByFieldNumber(t *testing.T) {
	t.Parallel()

	const opt = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:    new("test/order/v1/order.proto"),
		Package: new("test.order.v1"),
		Syntax:  new("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: new("Event"),
				// Declared out of numeric order: 3, 1, 2.
				Field: []*descriptorpb.FieldDescriptorProto{
					fieldProto("gamma", 3, opt, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
					fieldProto("alpha", 1, opt, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
					fieldProto("beta", 2, opt, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				},
			},
		},
	}

	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	files, err := protodesc.NewFiles(set)
	require.NoError(t, err)
	desc, err := files.FindDescriptorByName("test.order.v1.Event")
	require.NoError(t, err)
	msg, ok := desc.(protoreflect.MessageDescriptor)
	require.True(t, ok)

	schema, err := bigqueryTableSchema(msg)
	require.NoError(t, err)

	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, []byte(schema)))
	require.Equal(t,
		`[`+
			`{"name":"alpha","type":"STRING","mode":"NULLABLE"},`+
			`{"name":"beta","type":"STRING","mode":"NULLABLE"},`+
			`{"name":"gamma","type":"STRING","mode":"NULLABLE"}`+
			`]`,
		compact.String(),
	)
}

// bqSinkDescriptors builds a descriptor set with a topic message (Event) and a
// BigQuery sink marker (EventSink) carrying the given sink options.
func bqSinkDescriptors(t *testing.T, sink *pubsubv1.BigQuerySinkOptions) []byte {
	t.Helper()

	const pkg = "test.bq.v1"

	topicOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(topicOpts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	sinkOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(sinkOpts, pubsubv1.E_Subscription, pubsubv1.SubscriptionOptions_builder{
		Topic:    new(pkg + ".Event"),
		Bigquery: sink,
	}.Build())

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:       new("test/bq/v1/sink.proto"),
		Package:    new(pkg),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    new("Event"),
				Options: topicOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					fieldProto("id", 1, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				},
			},
			{Name: new("EventSink"), Options: sinkOpts},
		},
	}

	set := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
			protodesc.ToFileDescriptorProto(durationpb.File_google_protobuf_duration_proto),
			protodesc.ToFileDescriptorProto(pubsubv1.File_gcp_pubsub_v1_options_proto),
			fileProto,
		},
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)
	return raw
}

func TestDiscoverBigQuery_DatasetAndTable(t *testing.T) {
	t.Parallel()

	raw := bqSinkDescriptors(t, pubsubv1.BigQuerySinkOptions_builder{
		DropUnknownFields: new(true),
	}.Build())

	datasets, tables, err := DiscoverBigQuery(raw)
	require.NoError(t, err)

	require.Len(t, datasets, 1)
	require.Equal(t, "test_bq_v1", datasets[0].Name)
	require.Equal(t, managedByLabel, datasets[0].Labels["managed_by"])

	require.Len(t, tables, 1)
	table := tables[0]
	require.Equal(t, "event_sink", table.Name)
	require.Equal(t, "test_bq_v1", table.Dataset)
	require.Equal(t, "test.bq.v1.EventSink", table.ProtoMessage)

	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, []byte(table.Schema)))
	require.Equal(t, `[{"name":"id","type":"STRING","mode":"NULLABLE"}]`, compact.String())

	// Unset partition_expiration defaults to 60 days.
	require.True(t, table.PartitionExpirationEnabled)
	require.Equal(t, 60*24*time.Hour, table.PartitionExpiration)
}

func TestDiscoverBigQuery_PartitionExpiration(t *testing.T) {
	t.Parallel()

	t.Run("explicit value", func(t *testing.T) {
		t.Parallel()
		raw := bqSinkDescriptors(t, pubsubv1.BigQuerySinkOptions_builder{
			PartitionExpiration: durationpb.New(90 * 24 * time.Hour),
		}.Build())
		_, tables, err := DiscoverBigQuery(raw)
		require.NoError(t, err)
		require.Len(t, tables, 1)
		require.True(t, tables[0].PartitionExpirationEnabled)
		require.Equal(t, 90*24*time.Hour, tables[0].PartitionExpiration)
	})

	t.Run("zero disables", func(t *testing.T) {
		t.Parallel()
		raw := bqSinkDescriptors(t, pubsubv1.BigQuerySinkOptions_builder{
			PartitionExpiration: durationpb.New(0),
		}.Build())
		_, tables, err := DiscoverBigQuery(raw)
		require.NoError(t, err)
		require.Len(t, tables, 1)
		require.False(t, tables[0].PartitionExpirationEnabled)
	})
}

// TestDiscoverPubSub_BigQuerySinkRejectsDeadLetter confirms a BigQuery sink that
// also declares a dead_letter policy fails generation.
func TestDiscoverPubSub_BigQuerySinkRejectsDeadLetter(t *testing.T) {
	t.Parallel()

	const pkg = "test.bq.v1"

	topicOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(topicOpts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	sinkOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(sinkOpts, pubsubv1.E_Subscription, pubsubv1.SubscriptionOptions_builder{
		Topic:    new(pkg + ".Event"),
		Bigquery: pubsubv1.BigQuerySinkOptions_builder{}.Build(),
		DeadLetter: pubsubv1.DeadLetterPolicy_builder{
			MaxDeliveryAttempts: new(int32(5)),
		}.Build(),
	}.Build())

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:       new("test/bq/v1/sink.proto"),
		Package:    new(pkg),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Event"), Options: topicOpts},
			{Name: new("EventSink"), Options: sinkOpts},
		},
	}

	set := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
			protodesc.ToFileDescriptorProto(durationpb.File_google_protobuf_duration_proto),
			protodesc.ToFileDescriptorProto(pubsubv1.File_gcp_pubsub_v1_options_proto),
			fileProto,
		},
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, _, err = DiscoverPubSub(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not declare a dead_letter")
}

func TestBuildBigQueryValues(t *testing.T) {
	t.Parallel()

	datasets := []DesiredBigQueryDataset{
		{Name: "test_bq_v1", Labels: map[string]string{"managed_by": managedByLabel}},
	}
	tables := []DesiredBigQueryTable{
		{
			Name:                       "event_sink",
			Dataset:                    "test_bq_v1",
			Schema:                     `[{"name":"id","type":"STRING","mode":"NULLABLE"}]`,
			ProtoMessage:               "test.bq.v1.EventSink",
			PartitionExpiration:        60 * 24 * time.Hour,
			PartitionExpirationEnabled: true,
			Labels:                     map[string]string{"managed_by": managedByLabel},
		},
	}

	values := buildBigQueryValues(datasets, tables)

	require.True(t, values.Enabled)
	require.Equal(t, []string{bigqueryAPI}, values.APIs)

	require.Len(t, values.Tables, 1)
	table := values.Tables[0]
	require.Equal(t, "event_sink", table.Name)
	require.NotNil(t, table.Spec.Schema)
	require.Equal(t, "test_bq_v1", table.Spec.DatasetRef.Name)
	require.NotNil(t, table.Spec.TimePartitioning)
	require.Equal(t, timePartitioningMonth, table.Spec.TimePartitioning.Type)
	require.NotNil(t, table.Spec.TimePartitioning.ExpirationMs)
	require.Equal(t, int64(5184000000), *table.Spec.TimePartitioning.ExpirationMs)
	require.Equal(t, "test-bq-v1-event-sink", table.Labels[protoMessageLabel])
}

func TestBuildBigQueryValues_Empty(t *testing.T) {
	t.Parallel()

	values := buildBigQueryValues(nil, nil)
	require.False(t, values.Enabled)
	require.Empty(t, values.APIs)
	require.Empty(t, values.Tables)
	require.Empty(t, values.Datasets)
}
