package gcp

import (
	"os/exec"
	"path/filepath"
	"testing"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestScrubSchemaDefinition(t *testing.T) {
	t.Parallel()

	src := `edition = "2024";

// leading comment
package test.v1;

import "gcp/pubsub/v1/options.proto";

option go_package = "example.com/x;v1"; // trailing comment

/* a block
   comment */
message Foo {
  string a = 1; // inline comment
  int64 b = 2;

  option (gcp.pubsub.v1.topic) = {
    retention_hint: {
      seconds: 60 /* one minute */
    }
  };
}
`

	want := `edition = "2024";
message Foo {
  string a = 1;
  int64 b = 2;
}
`

	got, err := scrubSchemaDefinition(src)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestScrubSchemaDefinition_KeepsMessageLevelOption verifies that file-level
// directives (package, go_package) are stripped while a message's own option
// (here, deprecated) is preserved as part of the type definition.
func TestScrubSchemaDefinition_KeepsMessageLevelOption(t *testing.T) {
	t.Parallel()

	src := `edition = "2024";
package test.v1;
import "gcp/pubsub/v1/options.proto";
option go_package = "example.com/x;v1";
message Foo {
  option deprecated = true;
  string a = 1;
  option (gcp.pubsub.v1.topic) = { retention_hint: { seconds: 60 } };
}
`

	want := `edition = "2024";
message Foo {
  option deprecated = true;
  string a = 1;
}
`

	got, err := scrubSchemaDefinition(src)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestScrubSchemaDefinition_SingleLineOption ensures the topic option block is
// removed even when declared on a single line.
func TestScrubSchemaDefinition_SingleLineOption(t *testing.T) {
	t.Parallel()

	src := `edition = "2024";
package test.v1;
import "gcp/pubsub/v1/options.proto";
message Foo {
  string a = 1;
  option (gcp.pubsub.v1.topic) = { retention_hint: { seconds: 60 } };
}
`

	want := `edition = "2024";
message Foo {
  string a = 1;
}
`

	got, err := scrubSchemaDefinition(src)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestScrubSchemaDefinition_MultiLineFileOption confirms a multi-line file-level
// option is removed in full — its body must not leak into the scrubbed output.
func TestScrubSchemaDefinition_MultiLineFileOption(t *testing.T) {
	t.Parallel()

	src := `edition = "2024";
package test.v1;
import "gcp/pubsub/v1/options.proto";

option (custom.thing) = {
  alpha: "x"
  beta: { gamma: 1 }
};

message Foo {
  string a = 1;
  option (gcp.pubsub.v1.topic) = {};
}
`

	want := `edition = "2024";
message Foo {
  string a = 1;
}
`

	got, err := scrubSchemaDefinition(src)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestScrubSchemaDefinition_InlineBlockComment confirms a block comment between
// tokens is replaced by whitespace so the tokens stay separated.
func TestScrubSchemaDefinition_InlineBlockComment(t *testing.T) {
	t.Parallel()

	src := `edition = "2024";
message Foo {
  repeated /* note */ string tags = 1;
  option (gcp.pubsub.v1.topic) = {};
}
`

	got, err := scrubSchemaDefinition(src)
	require.NoError(t, err)
	require.NotContains(t, got, "repeatedstring")
	require.Contains(t, got, "string tags = 1;")
}

// TestScrubSchemaDefinition_MissingOption fails when no topic option is present,
// since every qualifying source must carry one.
func TestScrubSchemaDefinition_MissingOption(t *testing.T) {
	t.Parallel()

	src := `edition = "2024";
package test.v1;
message Foo {
  string a = 1;
}
`

	_, err := scrubSchemaDefinition(src)
	require.Error(t, err)
}

func TestBuildPubSubValues_Schemas(t *testing.T) {
	t.Parallel()

	schemas := []DesiredSchema{
		{
			Name:         "zebra-schema",
			ProtoMessage: "example.v1.Zebra",
			Definition:   "edition = \"2024\";\n",
			Labels:       map[string]string{"managed_by": managedByLabel},
		},
		{
			Name:         "alpha-schema",
			ProtoMessage: "example.v1.Alpha",
			Definition:   "edition = \"2024\";\n",
			Labels:       map[string]string{"managed_by": managedByLabel, deprecatedLabelKey: deprecatedLabelValue},
		},
	}

	doc := buildPubSubValues([]DesiredTopic{}, []DesiredSubscription{}, schemas)

	// Sorted alphabetically by name for stable diffs.
	require.Len(t, doc.PubSub.Schemas, 2)
	require.Equal(t, "alpha-schema", doc.PubSub.Schemas[0].Name)
	require.Equal(t, "zebra-schema", doc.PubSub.Schemas[1].Name)

	// Minimal spec: type plus the inlined definition.
	require.Equal(t, schemaTypeProtocolBuffer, doc.PubSub.Schemas[0].Spec.Type)
	require.Equal(t, "edition = \"2024\";\n", doc.PubSub.Schemas[0].Spec.Definition)

	// proto_message label carries the kebab-cased message name alongside managed_by.
	require.Equal(t, "example-v1-alpha", doc.PubSub.Schemas[0].Labels[protoMessageLabel])
	require.Equal(t, managedByLabel, doc.PubSub.Schemas[0].Labels["managed_by"])
	require.Equal(t, deprecatedLabelValue, doc.PubSub.Schemas[0].Labels[deprecatedLabelKey])
	require.NotContains(t, doc.PubSub.Schemas[1].Labels, deprecatedLabelKey)
}

// wellKnownSchemaDeps are the descriptor files a discovery test's set needs so
// the registry (and any message-typed field) resolves.
func wellKnownSchemaDeps() []*descriptorpb.FileDescriptorProto {
	return []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
		protodesc.ToFileDescriptorProto(durationpb.File_google_protobuf_duration_proto),
		protodesc.ToFileDescriptorProto(pubsubv1.File_gcp_pubsub_v1_options_proto),
	}
}

// TestDiscoverSchemas verifies a self-contained, single-message topic proto
// produces a schema with the scrubbed definition. Requires buf on PATH for the
// compile validation step.
func TestDiscoverSchemas(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), qualifyingSchemaFile(t)),
	}

	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)

	require.Len(t, schemas, 1)
	require.Equal(t, "test-schema-v1-widget", schemas[0].Name)
	require.Equal(t, "test.schema.v1.Widget", schemas[0].ProtoMessage)
	require.Equal(t, managedByLabel, schemas[0].Labels["managed_by"])

	want := `edition = "2024";
message Widget {
  string id = 1;
  int64 size = 2;
}
`
	require.Equal(t, want, schemas[0].Definition)
}

// TestDiscoverSchemas_MultipleTopLevelMessages confirms a file declaring more
// than one top-level message fails generation. This is caught structurally
// before any source is read, so no buf is required.
func TestDiscoverSchemas_MultipleTopLevelMessages(t *testing.T) {
	t.Parallel()

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), multiMessageSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, err = DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.ErrorContains(t, err, "top-level messages")
}

// TestDiscoverSchemas_ImportedType confirms a topic message that references an
// imported/external type fails generation: scrubbing strips the import, so the
// type no longer resolves at the buf validation step. Requires buf on PATH.
func TestDiscoverSchemas_ImportedType(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), importedTypeSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, err = DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.ErrorContains(t, err, "validate schema definition")
}

// TestDiscoverSchemas_NoTopics verifies that when no file declares a topic the
// result is a non-nil empty slice, never nil.
func TestDiscoverSchemas_NoTopics(t *testing.T) {
	t.Parallel()

	set := &descriptorpb.FileDescriptorSet{File: wellKnownSchemaDeps()}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)
	require.NotNil(t, schemas)
	require.Empty(t, schemas)
}

// TestDiscoverSchemas_NestedMessage confirms a single top-level message that
// declares and uses a nested message is supported end to end: the nested type
// resolves locally and buf validates the self-contained definition. This is the
// positive counterpart to TestDiscoverSchemas_ImportedType. Requires buf.
func TestDiscoverSchemas_NestedMessage(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), nestedMessageSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)

	require.Len(t, schemas, 1)
	require.Equal(t, "test-schema-v1-envelope", schemas[0].Name)
	require.Contains(t, schemas[0].Definition, "message Header")
}

// TestDiscoverSchemas_TopLevelEnum confirms a topic file that also declares a
// top-level enum fails: a schema must be exactly one top-level message and
// nothing else. Caught structurally, so no buf is required.
func TestDiscoverSchemas_TopLevelEnum(t *testing.T) {
	t.Parallel()

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), topLevelEnumSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, err = DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.ErrorContains(t, err, "enums, services, or extensions")
}

// TestDiscoverSchemas_NonTopicFileIgnored confirms the structural errors apply
// only to files that declare a topic: a multi-message file with no topic option
// is skipped silently, never erroring.
func TestDiscoverSchemas_NonTopicFileIgnored(t *testing.T) {
	t.Parallel()

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), nonTopicMultiMessageFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)
	require.Empty(t, schemas)
}

// TestDiscoverSchemas_NestedTopicOption confirms a topic option on a nested
// message is rejected rather than silently ignored. Caught structurally, so no
// buf is required.
func TestDiscoverSchemas_NestedTopicOption(t *testing.T) {
	t.Parallel()

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), nestedTopicOptionFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, err = DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.ErrorContains(t, err, "nested message")
}

// TestDiscoverSchemas_IgnoresExplicitTopicName confirms the schema name is
// derived from the message name and is independent of an explicit `name` set on
// the topic option (schemas are not tied to topics). Requires buf.
func TestDiscoverSchemas_IgnoresExplicitTopicName(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), namedTopicSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)
	require.Len(t, schemas, 1)
	require.Equal(t, "test-schema-v1-named", schemas[0].Name)
	require.NotEqual(t, "custom-name", schemas[0].Name)
}

// TestDiscoverSchemas_Deprecated confirms a topic message marked
// `option deprecated = true` yields the deprecated label on its schema, and the
// message-level option survives scrubbing. Requires buf.
func TestDiscoverSchemas_Deprecated(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), deprecatedTopicSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)
	require.Len(t, schemas, 1)
	require.Equal(t, deprecatedLabelValue, schemas[0].Labels[deprecatedLabelKey])
	require.Contains(t, schemas[0].Definition, "option deprecated = true;")
}

// TestDiscoverSchemas_MapOfScalars confirms a map with scalar key and value is
// self-contained and supported. Requires buf.
func TestDiscoverSchemas_MapOfScalars(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), mapScalarSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	schemas, err := DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.NoError(t, err)
	require.Len(t, schemas, 1)
}

// TestDiscoverSchemas_MapWithExternalValue confirms a map whose value type is
// imported is rejected: stripping the import leaves the value type unresolved at
// validation. Requires buf.
func TestDiscoverSchemas_MapWithExternalValue(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	set := &descriptorpb.FileDescriptorSet{
		File: append(wellKnownSchemaDeps(), mapExternalSchemaFile(t)),
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, err = DiscoverSchemas(t.Context(), raw, filepath.Join("testdata", "proto"))
	require.ErrorContains(t, err, "validate schema definition")
}

// TestValidateSchemaDefinition_RejectsInvalid confirms a malformed definition
// fails generation rather than being emitted.
func TestValidateSchemaDefinition_RejectsInvalid(t *testing.T) {
	t.Parallel()
	requireBuf(t)

	invalid := `edition = "2024";
package test.v1;
message Broken {
  string a = ;
}
`

	err := validateSchemaDefinition(t.Context(), "test.v1.Broken", invalid)
	require.Error(t, err)
}

func requireBuf(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf not found on PATH; skipping schema validation test")
	}
}

func qualifyingSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/widget.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    new("Widget"),
				Options: opts,
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("id", 1),
					int64Field("size", 2),
				},
			},
		},
	}
}

func multiMessageSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/multi.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Primary"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
			{Name: new("Secondary"), Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

// importedTypeSchemaFile describes a single-message topic proto whose field
// references an imported type (google.protobuf.Duration). It pairs with the
// on-disk testdata/proto/test/schema/v1/withduration.proto source.
func importedTypeSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	durationField := stringField("ttl", 2)
	durationField.Type = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
	durationField.TypeName = new(".google.protobuf.Duration")

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/withduration.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto", "google/protobuf/duration.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("WithDuration"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1), durationField}},
		},
	}
}

// nestedMessageSchemaFile describes a single top-level message. The on-disk
// testdata/proto/test/schema/v1/nested.proto source carries the nested message
// and the field referencing it; the descriptor only needs to report one
// top-level message so discovery proceeds to read and validate that source.
func nestedMessageSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/nested.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Envelope"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

// topLevelEnumSchemaFile describes a single-message topic file that also
// declares a top-level enum, which must be rejected.
func topLevelEnumSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/withenum.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Foo"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{
			{
				Name:  new("Color"),
				Value: []*descriptorpb.EnumValueDescriptorProto{{Name: new("COLOR_UNSPECIFIED"), Number: new(int32(0))}},
			},
		},
	}
}

// nonTopicMultiMessageFile describes a file with multiple top-level messages and
// no topic option; discovery must skip it without erroring.
func nonTopicMultiMessageFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	return &descriptorpb.FileDescriptorProto{
		Name:    new("test/schema/v1/plain.proto"),
		Package: new("test.schema.v1"),
		Syntax:  new("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Alpha"), Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
			{Name: new("Beta"), Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

// nestedTopicOptionFile describes a top-level message whose nested message
// carries the topic option, which must be rejected.
func nestedTopicOptionFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/nestedtopic.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:       new("Outer"),
				Field:      []*descriptorpb.FieldDescriptorProto{stringField("id", 1)},
				NestedType: []*descriptorpb.DescriptorProto{{Name: new("Inner"), Options: opts}},
			},
		},
	}
}

// namedTopicSchemaFile sets an explicit topic name; the schema name must ignore
// it. Pairs with testdata/proto/test/schema/v1/named.proto.
func namedTopicSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{Name: new("custom-name")}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/named.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Named"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

// deprecatedTopicSchemaFile marks the topic message deprecated. Pairs with
// testdata/proto/test/schema/v1/deprecated.proto.
func deprecatedTopicSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{Deprecated: new(true)}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/deprecated.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("LegacyEvent"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

// mapScalarSchemaFile pairs with testdata/proto/test/schema/v1/map_scalar.proto;
// the map field lives in the on-disk source that buf validates.
func mapScalarSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/map_scalar.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Tags"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

// mapExternalSchemaFile pairs with testdata/proto/test/schema/v1/map_external.proto;
// the map's imported value type lives in the on-disk source that buf validates.
func mapExternalSchemaFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return &descriptorpb.FileDescriptorProto{
		Name:       new("test/schema/v1/map_external.proto"),
		Package:    new("test.schema.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Ttls"), Options: opts, Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}},
		},
	}
}

func stringField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   new(name),
		Number: new(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

func int64Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   new(name),
		Number: new(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}
