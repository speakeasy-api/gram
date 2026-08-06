package gcp

import (
	"fmt"
	"go/token"
	"path"
	"sort"
	"strings"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TopicBinding ties a topic-declaring proto message to the Go type generated
// for it, which is what lets a topic be named at runtime while still resolving
// to a statically typed publisher.
type TopicBinding struct {
	// ProtoFullName is the message's fully qualified proto name, e.g.
	// "gram.webhooks.v1.Event". This is the value stored on an outbox row.
	ProtoFullName string
	// TopicID is the resolved Pub/Sub topic id.
	TopicID string
	// GoImportPath is the import path of the generated Go package.
	GoImportPath string
	// GoPackageAlias is the package's preferred identifier, e.g. "webhooksv1".
	GoPackageAlias string
	// GoTypeName is the generated struct name. Nested messages are joined with
	// underscores, matching protoc-gen-go.
	GoTypeName string
	// ConstName is the exported Go identifier for this topic's constant.
	ConstName string
}

// DiscoverTopicBindings walks a descriptor set and returns one binding per
// message declaring a (gcp.pubsub.v1.topic) option, sorted by proto name so the
// generated output is stable.
func DiscoverTopicBindings(descriptorBytes []byte) ([]TopicBinding, error) {
	var descriptorSet descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(descriptorBytes, &descriptorSet); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&descriptorSet)
	if err != nil {
		return nil, fmt.Errorf("build proto file registry: %w", err)
	}

	var (
		bindings []TopicBinding
		walkErr  error
	)

	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		importPath, alias, err := goPackageOf(file)
		if err != nil {
			walkErr = err
			return false
		}

		walkErr = collectTopicBindings(file.Messages(), importPath, alias, nil, &bindings)

		return walkErr == nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Slice(bindings, func(i, j int) bool {
		return bindings[i].ProtoFullName < bindings[j].ProtoFullName
	})

	// constNameFor flattens dots without a separator, so distinct proto names
	// can collapse onto one identifier — nested "a.v1.Bar.Baz" and top-level
	// "a.v1.BarBaz" both become AV1BarBaz. Left undetected, generation emits
	// duplicate constants: still valid template output, so the break would only
	// surface as a compile error in the generated file.
	constOwner := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		if other, taken := constOwner[binding.ConstName]; taken {
			return nil, fmt.Errorf("topics %s and %s both generate the constant %s; rename one of the messages", other, binding.ProtoFullName, binding.ConstName)
		}
		constOwner[binding.ConstName] = binding.ProtoFullName
	}

	return bindings, nil
}

func collectTopicBindings(messages protoreflect.MessageDescriptors, importPath, alias string, parents []string, out *[]TopicBinding) error {
	for i := range messages.Len() {
		message := messages.Get(i)
		name := string(message.Name())

		if options, ok := TopicOptionsFromMessage(message); ok {
			if importPath == "" {
				return fmt.Errorf("message %s declares a topic but its file sets no go_package", message.FullName())
			}

			*out = append(*out, TopicBinding{
				ProtoFullName:  string(message.FullName()),
				TopicID:        ResolveTopicName(message, options),
				GoImportPath:   importPath,
				GoPackageAlias: alias,
				GoTypeName:     strings.Join(append(append([]string{}, parents...), name), "_"),
				ConstName:      constNameFor(message.FullName()),
			})
		}

		if err := collectTopicBindings(message.Messages(), importPath, alias, append(parents, name), out); err != nil {
			return err
		}
	}

	return nil
}

// goPackageOf splits a file's go_package option into its import path and
// preferred package identifier. The "path;alias" form wins; otherwise the last
// path element is the identifier.
func goPackageOf(file protoreflect.FileDescriptor) (importPath, alias string, err error) {
	options, ok := file.Options().(*descriptorpb.FileOptions)
	if !ok || options == nil {
		return "", "", nil
	}

	goPackage := strings.TrimSpace(options.GetGoPackage())
	if goPackage == "" {
		return "", "", nil
	}

	importPath, alias, found := strings.Cut(goPackage, ";")
	if !found {
		alias = path.Base(importPath)
	}

	if importPath == "" {
		return "", "", fmt.Errorf("file %s has a malformed go_package option %q", file.Path(), goPackage)
	}

	return importPath, sanitizeAlias(alias), nil
}

// sanitizeAlias coerces an alias into a valid Go identifier the same way
// protoc-gen-go cleans package names: illegal runes become underscores, a
// leading digit gets a prefix, and keywords get a suffix. Without this, a
// go_package like "example.com/gen/foo-bar" (legal for protoc-gen-go, which
// sanitizes it itself) templates a malformed import and generation dies in
// format.Source with a parse error that names neither the proto file nor the
// alias that caused it.
func sanitizeAlias(alias string) string {
	sanitized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return r
		}
		return '_'
	}, alias)

	if sanitized == "" {
		return "_"
	}
	if unicode.IsDigit([]rune(sanitized)[0]) {
		sanitized = "_" + sanitized
	}
	if !token.IsIdentifier(sanitized) {
		// Everything structural is already fixed, so this is a Go keyword.
		sanitized += "_"
	}

	return sanitized
}

// constNameFor turns a proto full name into an exported Go identifier, e.g.
// "gram.webhooks.v1.Event" becomes "GramWebhooksV1Event".
func constNameFor(fullName protoreflect.FullName) string {
	var b strings.Builder

	for part := range strings.SplitSeq(string(fullName), ".") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}

	return b.String()
}
