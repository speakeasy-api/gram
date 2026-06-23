package gcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ettle/strcase"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// schemaTypeProtocolBuffer is the Config Connector PubSubSchema `type` value for
// protobuf schema definitions.
const schemaTypeProtocolBuffer = "PROTOCOL_BUFFER"

// topicOptionMarker is the leading text of the message-level option that
// declares a topic. The whole `option (...) = { ... };` block is scrubbed from
// the inlined schema definition since it depends on the pubsub options import.
const topicOptionMarker = "option (gcp.pubsub.v1.topic)"

// DesiredSchema is a PubSubSchema resource derived from a self-contained topic
// proto: the inlined, scrubbed protobuf definition plus traceability labels. The
// schema is intentionally not tied to its topic; it is provisioned on its own.
type DesiredSchema struct {
	Name         string
	ProtoMessage string
	Definition   string
	Labels       map[string]string
}

// DiscoverSchemas walks the descriptor set and, for every file that declares a
// topic, produces a PubSubSchema carrying the inlined protobuf definition. The
// original proto source is read from protoRoot (joined with the descriptor's
// file path) since descriptors do not retain source text.
//
// Generation fails (rather than silently skipping) when a topic proto cannot be
// expressed as a single self-contained schema: when its file declares more than
// one top-level message, and — via the post-scrub buf compile, which sees the
// definition with all imports removed — when the message references an
// imported/external type.
func DiscoverSchemas(ctx context.Context, descriptorBytes []byte, protoRoot string) ([]DesiredSchema, error) {
	var descriptorSet descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(descriptorBytes, &descriptorSet); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&descriptorSet)
	if err != nil {
		return nil, fmt.Errorf("build proto file registry: %w", err)
	}

	// Initialize non-nil so callers always receive an empty slice rather than nil
	// when no topic protos qualify, keeping buildPubSubValues' input explicit.
	schemas := make([]DesiredSchema, 0)
	var walkErr error
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		walkErr = collectSchemaFromFile(ctx, file, protoRoot, &schemas)
		return walkErr == nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return schemas, nil
}

func collectSchemaFromFile(ctx context.Context, file protoreflect.FileDescriptor, protoRoot string, schemas *[]DesiredSchema) error {
	messages := file.Messages()

	// A topic option on a nested message is almost certainly a mistake: topics
	// (and thus schemas) are derived from top-level messages. Reject it rather
	// than silently ignore it.
	if nested := nestedTopicMessage(messages); nested != nil {
		return fmt.Errorf("schema source: message %s declares a topic option on a nested message; topic options must be declared on a top-level message", nested.FullName())
	}

	var topicMessage protoreflect.MessageDescriptor
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)
		if _, ok := TopicOptionsFromMessage(message); ok {
			topicMessage = message
			break
		}
	}
	if topicMessage == nil {
		// No top-level topic here (a subscription marker, an imported well-known
		// type, or a plain message); nothing to emit.
		return nil
	}

	// A schema must be a single self-contained top-level type. More than one
	// top-level message — or any top-level enum, service, or extension — cannot
	// be expressed as one PubSubSchema definition, so fail loudly rather than
	// guess which type to inline. Nested messages are fine.
	if messages.Len() > 1 {
		return fmt.Errorf("schema source for %s: file %s declares %d top-level messages; a schema proto must declare exactly one top-level message", topicMessage.FullName(), file.Path(), messages.Len())
	}
	if file.Enums().Len() > 0 || file.Services().Len() > 0 || file.Extensions().Len() > 0 {
		return fmt.Errorf("schema source for %s: file %s declares top-level enums, services, or extensions; a schema proto must declare exactly one top-level message and nothing else", topicMessage.FullName(), file.Path())
	}

	srcPath := filepath.Join(protoRoot, file.Path())
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read proto source %s for schema %s: %w", srcPath, topicMessage.FullName(), err)
	}

	definition, err := scrubSchemaDefinition(string(raw))
	if err != nil {
		return fmt.Errorf("scrub schema definition for %s: %w", topicMessage.FullName(), err)
	}

	// Validation compiles the scrubbed definition, which has had all imports
	// removed. A field referencing an imported/external type (e.g.
	// google.protobuf.Timestamp) therefore no longer resolves and fails here —
	// this is how complex, non-self-contained types are rejected.
	if err := validateSchemaDefinition(ctx, string(topicMessage.FullName()), definition); err != nil {
		return err
	}

	labels := map[string]string{"managed_by": managedByLabel}
	if messageDeprecated(topicMessage) {
		labels[deprecatedLabelKey] = deprecatedLabelValue
	}

	*schemas = append(*schemas, DesiredSchema{
		// A PubSubSchema describes the shape of one protobuf message, so its name
		// has affinity to that message — the kebab-cased proto full name — never
		// to the topic option's `name`. That `name` field is a routing override
		// for publishing a message to a shared, externally-owned topic (so
		// several messages can map onto one topic ID); binding a schema to it
		// would collide or orphan schemas when topics are shared or renamed.
		Name:         strcase.ToKebab(string(topicMessage.FullName())),
		ProtoMessage: string(topicMessage.FullName()),
		Definition:   definition,
		Labels:       labels,
	})

	return nil
}

// nestedTopicMessage returns the first message below the top level that carries
// a topic option, or nil if none do. Top-level messages are not considered —
// they are the valid place for a topic option.
func nestedTopicMessage(messages protoreflect.MessageDescriptors) protoreflect.MessageDescriptor {
	for i := 0; i < messages.Len(); i++ {
		if found := topicInTree(messages.Get(i).Messages()); found != nil {
			return found
		}
	}
	return nil
}

func topicInTree(messages protoreflect.MessageDescriptors) protoreflect.MessageDescriptor {
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)
		if _, ok := TopicOptionsFromMessage(message); ok {
			return message
		}
		if found := topicInTree(message.Messages()); found != nil {
			return found
		}
	}
	return nil
}

// scrubSchemaDefinition turns a topic proto's source into a compact, standalone
// schema definition that GCP Pub/Sub accepts: the edition declaration followed
// by the single top-level message, with comments and blank lines removed and the
// message-level topic option block stripped out.
//
// It emits exactly those two constructs rather than deleting unwanted file-level
// statements one by one. This is what keeps the result self-contained — the
// package directive, all imports, and any file-level option (including a
// multi-line aggregate option whose body spans several lines) are simply never
// emitted. Dropping imports while keeping the message is also how external types
// are rejected: a field referencing an imported type is retained but its import
// is gone, so it fails to resolve at the validation step.
func scrubSchemaDefinition(src string) (string, error) {
	decommented := stripProtoComments(src)

	withoutOption, err := removeTopicOptionBlock(decommented)
	if err != nil {
		return "", err
	}

	edition := editionDeclaration(withoutOption)
	if edition == "" {
		return "", errors.New("no edition or syntax declaration found in schema source")
	}

	message, err := topLevelMessageBlock(withoutOption)
	if err != nil {
		return "", err
	}

	out := []string{edition}
	for line := range strings.SplitSeq(message, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}

	return strings.Join(out, "\n") + "\n", nil
}

// editionDeclaration returns the file's `edition = ...;` (or `syntax = ...;`)
// line, trimmed of trailing whitespace, or "" if absent. It is always the file's
// first statement, so the first matching line is the declaration.
func editionDeclaration(src string) string {
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "edition") || strings.HasPrefix(trimmed, "syntax") {
			return strings.TrimRight(line, " \t")
		}
	}
	return ""
}

// topLevelMessageBlock returns the source of the single top-level message, from
// its `message X {` line through the matching closing brace. The caller has
// already verified the file declares exactly one top-level message, so the first
// `message ` line is it — a message's own nested messages are declared inside its
// body, which is reached only after this line.
func topLevelMessageBlock(src string) (string, error) {
	lines := strings.Split(src, "\n")

	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "message ") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", errors.New("no top-level message found in schema source")
	}

	depth := 0
	opened := false
	for i := start; i < len(lines); i++ {
		depth += netBraceDelta(lines[i])
		if depth > 0 {
			opened = true
		}
		if opened && depth <= 0 {
			return strings.Join(lines[start:i+1], "\n"), nil
		}
	}

	return "", errors.New("unterminated top-level message in schema source")
}

// netBraceDelta returns a line's `{` count minus its `}` count, ignoring braces
// inside string literals. The input is expected to already be comment-stripped.
func netBraceDelta(line string) int {
	delta := 0
	inString := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString {
			switch c {
			case '\\':
				i++
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

// stripProtoComments removes `//` line comments and `/* */` block comments while
// preserving the content of string literals (so a `//` or `/*` inside a quoted
// value is never mistaken for a comment).
func stripProtoComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))

	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]

		if inString {
			b.WriteByte(c)
			switch c {
			case '\\':
				if i+1 < len(src) {
					b.WriteByte(src[i+1])
					i++
				}
			case '"':
				inString = false
			}
			continue
		}

		switch {
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			// Line comment: skip to (but keep) the end of line.
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			// Block comment: replace it with a single space so adjacent tokens
			// stay separated (a block comment is whitespace in proto, so
			// "repeated/* */string" must not become "repeatedstring"), then skip
			// to the closing "*/".
			b.WriteByte(' ')
			i += 2
			for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			i++ // land on the '/' so the loop's increment steps past it
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

// removeTopicOptionBlock removes the `option (gcp.pubsub.v1.topic) = { ... };`
// statement, including the trailing semicolon. Brace matching skips string
// literals so a brace inside a quoted value cannot end the block early. The
// input is expected to already be comment-stripped.
func removeTopicOptionBlock(src string) (string, error) {
	start := strings.Index(src, topicOptionMarker)
	if start < 0 {
		return "", fmt.Errorf("topic option %q not found in schema source", topicOptionMarker)
	}

	open := strings.IndexByte(src[start:], '{')
	if open < 0 {
		return "", errors.New("topic option opening brace not found")
	}
	open += start

	depth := 0
	inString := false
	for i := open; i < len(src); i++ {
		c := src[i]

		if inString {
			switch c {
			case '\\':
				i++
			case '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end := i + 1
				for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
					end++
				}
				if end < len(src) && src[end] == ';' {
					end++
				}
				return src[:start] + src[end:], nil
			}
		}
	}

	return "", errors.New("unterminated topic option block")
}

// validateSchemaDefinition compiles a scrubbed definition with the buf CLI to
// confirm it is valid protobuf before it is emitted. buf is already a hard
// dependency of the generation pipeline and, unlike Go parser libraries,
// supports the proto editions these definitions use.
func validateSchemaDefinition(ctx context.Context, protoMessage, definition string) error {
	dir, err := os.MkdirTemp("", "pubsub-schema-validate-*")
	if err != nil {
		return fmt.Errorf("create temp dir for schema validation: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	const bufConfig = "version: v2\nmodules:\n  - path: .\n"
	if err := os.WriteFile(filepath.Join(dir, "buf.yaml"), []byte(bufConfig), 0o600); err != nil {
		return fmt.Errorf("write buf.yaml for schema validation: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.proto"), []byte(definition), 0o600); err != nil {
		return fmt.Errorf("write schema.proto for schema validation: %w", err)
	}

	cmd := exec.CommandContext(ctx, "buf", "build")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("validate schema definition for %s: %w: %s", protoMessage, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}
