package gcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/speakeasy-api/gram/infra/gen"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
)

func topicOptions(t *testing.T) *descriptorpb.MessageOptions {
	t.Helper()

	opts := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	return opts
}

// bindingsFrom discovers bindings for synthetic test files, returning only
// those in the test package. The real descriptor set is included because the
// options proto and its own transitive dependencies have to resolve; filtering
// on the package keeps these assertions independent of the topics the
// repository actually declares.
func bindingsFrom(t *testing.T, files ...*descriptorpb.FileDescriptorProto) []TopicBinding {
	t.Helper()

	var embedded descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(gen.Descriptors, &embedded))

	set := &descriptorpb.FileDescriptorSet{File: append(embedded.GetFile(), files...)}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	all, err := DiscoverTopicBindings(raw)
	require.NoError(t, err)

	var bindings []TopicBinding
	for _, binding := range all {
		if strings.HasPrefix(binding.ProtoFullName, "test.topics.v1.") {
			bindings = append(bindings, binding)
		}
	}

	return bindings
}

func TestDiscoverTopicBindings_DerivesGoBinding(t *testing.T) {
	t.Parallel()

	bindings := bindingsFrom(t, &descriptorpb.FileDescriptorProto{
		Name:       new("test/topics/v1/thing.proto"),
		Package:    new("test.topics.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/gen/test/topics/v1;topicsv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Thing"), Options: topicOptions(t)},
		},
	})

	require.Len(t, bindings, 1)
	require.Equal(t, "test.topics.v1.Thing", bindings[0].ProtoFullName)
	require.Equal(t, "example.com/gen/test/topics/v1", bindings[0].GoImportPath)
	require.Equal(t, "topicsv1", bindings[0].GoPackageAlias)
	require.Equal(t, "Thing", bindings[0].GoTypeName)
	require.Equal(t, "TestTopicsV1Thing", bindings[0].ConstName)
}

// TestDiscoverTopicBindings_SanitizesDerivedAlias covers a go_package with no
// explicit alias whose last path element is not a valid Go identifier —
// protoc-gen-go accepts these and sanitizes the package name itself, so the
// registry generator must too or it dies in format.Source with a parse error
// naming neither the proto file nor the alias.
func TestDiscoverTopicBindings_SanitizesDerivedAlias(t *testing.T) {
	t.Parallel()

	bindings := bindingsFrom(t, &descriptorpb.FileDescriptorProto{
		Name:       new("test/topics/v1/dashed.proto"),
		Package:    new("test.topics.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/gen/foo-bar")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Thing"), Options: topicOptions(t)},
		},
	})

	require.Len(t, bindings, 1)
	require.Equal(t, "example.com/gen/foo-bar", bindings[0].GoImportPath)
	require.Equal(t, "foo_bar", bindings[0].GoPackageAlias)
}

func TestDiscoverTopicBindings_IgnoresMessagesWithoutTopics(t *testing.T) {
	t.Parallel()

	bindings := bindingsFrom(t, &descriptorpb.FileDescriptorProto{
		Name:       new("test/topics/v1/mixed.proto"),
		Package:    new("test.topics.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/gen/test/topics/v1;topicsv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("HasTopic"), Options: topicOptions(t)},
			{Name: new("PlainMessage")},
		},
	})

	require.Len(t, bindings, 1)
	require.Equal(t, "test.topics.v1.HasTopic", bindings[0].ProtoFullName)
}

// TestDiscoverTopicBindings_NestedMessageGoName pins the protoc-gen-go naming
// rule for nested messages. Getting it wrong would emit a registry that does not
// compile, which is the failure mode generation is meant to prevent.
func TestDiscoverTopicBindings_NestedMessageGoName(t *testing.T) {
	t.Parallel()

	bindings := bindingsFrom(t, &descriptorpb.FileDescriptorProto{
		Name:       new("test/topics/v1/nested.proto"),
		Package:    new("test.topics.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/gen/test/topics/v1;topicsv1")},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:       new("Outer"),
			NestedType: []*descriptorpb.DescriptorProto{{Name: new("Inner"), Options: topicOptions(t)}},
		}},
	})

	require.Len(t, bindings, 1)
	require.Equal(t, "test.topics.v1.Outer.Inner", bindings[0].ProtoFullName)
	require.Equal(t, "Outer_Inner", bindings[0].GoTypeName)
	require.Equal(t, "TestTopicsV1OuterInner", bindings[0].ConstName)
}

// TestDiscoverTopicBindings_RejectsConstNameCollisions covers two distinct
// proto names that flatten onto one Go identifier. Generation must refuse with
// an error naming both, rather than emit duplicate constants that break the
// build of every binary linking the registry.
func TestDiscoverTopicBindings_RejectsConstNameCollisions(t *testing.T) {
	t.Parallel()

	var embedded descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(gen.Descriptors, &embedded))

	set := &descriptorpb.FileDescriptorSet{File: append(embedded.GetFile(), &descriptorpb.FileDescriptorProto{
		Name:       new("test/topics/v1/collide.proto"),
		Package:    new("test.topics.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/gen/test/topics/v1;topicsv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:       new("Bar"),
				NestedType: []*descriptorpb.DescriptorProto{{Name: new("Baz"), Options: topicOptions(t)}},
			},
			{Name: new("BarBaz"), Options: topicOptions(t)},
		},
	})}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	_, err = DiscoverTopicBindings(raw)
	require.ErrorContains(t, err, "TestTopicsV1BarBaz")
	require.ErrorContains(t, err, "test.topics.v1.Bar.Baz")
	require.ErrorContains(t, err, "test.topics.v1.BarBaz")
}

func TestDiscoverTopicBindings_SortedByProtoName(t *testing.T) {
	t.Parallel()

	bindings := bindingsFrom(t, &descriptorpb.FileDescriptorProto{
		Name:       new("test/topics/v1/many.proto"),
		Package:    new("test.topics.v1"),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/gen/test/topics/v1;topicsv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Zebra"), Options: topicOptions(t)},
			{Name: new("Alpha"), Options: topicOptions(t)},
		},
	})

	require.Len(t, bindings, 2)
	require.Equal(t, "test.topics.v1.Alpha", bindings[0].ProtoFullName)
	require.Equal(t, "test.topics.v1.Zebra", bindings[1].ProtoFullName)
}

// TestBuildTopicsTemplateData_DisambiguatesAliases covers two packages that
// prefer the same identifier, which would otherwise emit a file with duplicate
// import names.
func TestBuildTopicsTemplateData_DisambiguatesAliases(t *testing.T) {
	t.Parallel()

	data, err := buildTopicsTemplateData([]TopicBinding{
		{ProtoFullName: "a.v1.A", GoImportPath: "example.com/a/v1", GoPackageAlias: "v1", GoTypeName: "A", ConstName: "AV1A"},
		{ProtoFullName: "b.v1.B", GoImportPath: "example.com/b/v1", GoPackageAlias: "v1", GoTypeName: "B", ConstName: "BV1B"},
	})
	require.NoError(t, err)

	require.Len(t, data.Imports, 2)
	require.NotEqual(t, data.Imports[0].Alias, data.Imports[1].Alias)

	byPath := map[string]string{}
	for _, imp := range data.Imports {
		byPath[imp.Path] = imp.Alias
	}
	for _, binding := range data.Bindings {
		require.Equal(t, byPath[binding.GoImportPath], binding.GoPackageAlias,
			"each binding must reference the alias actually imported for its package")
	}
}

// TestBuildTopicsTemplateData_ReservesTemplateIdentifiers covers a package
// whose preferred alias matches an identifier the generated file already uses.
// Emitting it verbatim would shadow the fixed import: still valid syntax, so
// generation would succeed and the break would only show up as a repo-wide
// compile error.
func TestBuildTopicsTemplateData_ReservesTemplateIdentifiers(t *testing.T) {
	t.Parallel()

	data, err := buildTopicsTemplateData([]TopicBinding{
		{ProtoFullName: "a.v1.A", GoImportPath: "example.com/gcp/v1", GoPackageAlias: "gcp", GoTypeName: "A", ConstName: "AV1A"},
		{ProtoFullName: "b.v1.B", GoImportPath: "example.com/b/pubsub", GoPackageAlias: "pubsub", GoTypeName: "B", ConstName: "BV1B"},
		{ProtoFullName: "c.v1.C", GoImportPath: "example.com/c/topics", GoPackageAlias: "topics", GoTypeName: "C", ConstName: "CV1C"},
	})
	require.NoError(t, err)

	reserved := map[string]bool{"context": true, "fmt": true, "pubsub": true, "gcp": true, "topics": true}
	for _, imp := range data.Imports {
		require.Falsef(t, reserved[imp.Alias],
			"alias %q for %s collides with an identifier the generated file declares", imp.Alias, imp.Path)
	}
}

func TestRenderTopics_CompilesAgainstRealDescriptors(t *testing.T) {
	t.Parallel()

	rendered, err := RenderTopics(gen.Descriptors)
	require.NoError(t, err)

	// format.Source inside RenderTopics already rejects anything unparseable;
	// these assertions cover the parts a syntax check cannot.
	require.Contains(t, string(rendered), "// Code generated by infra gen-topics. DO NOT EDIT.")
	require.Contains(t, string(rendered), `GramRiskV1Finding Topic = "gram.risk.v1.Finding"`)
	require.NotContains(t, string(rendered), "Processor Topic =",
		"a subscription marker declares no topic and must not appear")
}

// TestCheckTopics_DetectsDrift is the guard that makes generation worth having:
// adding a topic proto without regenerating must fail rather than silently
// leave the new topic unpublishable.
func TestCheckTopics_DetectsDrift(t *testing.T) {
	t.Parallel()

	require.NoError(t, CheckTopics(gen.Descriptors, "../../pkg/topics/topics_gen.go"),
		"committed registry is stale; run: mise run gen:infra")
}
