package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Audience names a surface a tool may be served to.
//
// Membership is declared per tool rather than per catalogue. The two surfaces
// differ in identity, targeting, and safety, so a tool built for one is not
// automatically fit for the other: admitting a capability to the assistant is
// a deliberate act a reviewer can see in the diff, not a consequence of adding
// it to Platform MCP.
type Audience string

// AssistantClientID names the acting client on assistant-originated calls, so
// telemetry and audit can tell an assistant-driven change from an external
// client's.
const AssistantClientID = "gram-project-assistant"

const (
	// AudienceExternal is the OAuth-authenticated /platform-mcp endpoint.
	AudienceExternal Audience = "external"
	// AudienceAssistant is a project's managed assistant, which acts under
	// assistant identity rather than an external user's OAuth connection.
	AudienceAssistant Audience = "assistant"
)

// ProjectScope declares how a tool obtains the project it acts on.
type ProjectScope int

const (
	// ProjectScopeNone: the tool does not act on a single project.
	ProjectScopeNone ProjectScope = iota
	// ProjectScopeExplicit: the caller names the project. An external client
	// spans every project in its organization, so it has to say which.
	ProjectScopeExplicit
)

// ToolMeta is what a tool declares beyond its schemas: who may call it, and
// how it obtains its target.
type ToolMeta struct {
	Audiences    []Audience
	ProjectScope ProjectScope
}

func (m ToolMeta) servesAudience(audience Audience) bool {
	return slices.Contains(m.Audiences, audience)
}

// Descriptor is one registered tool, reachable either through the MCP server
// or by direct call.
//
// The direct path exists for surfaces that speak Go rather than MCP. Going
// through a second in-process MCP server instead would re-encode every call,
// and would flatten a refusal into an ordinary result on the way back.
type Descriptor struct {
	Name        string
	Title       string
	Description string
	Annotations *mcp.ToolAnnotations
	Meta        ToolMeta
	InputSchema []byte

	invoke func(ctx context.Context, arguments json.RawMessage) (any, error)
}

// ToolRefusalError is a tool's own refusal — a rate limit, a disabled feature, an
// ineligible target — rather than a failure of the call. The MCP surface
// returns these as an error result; a direct caller receives this error, so
// the reason survives instead of being replaced by an empty payload.
type ToolRefusalError struct {
	Payload string
}

func (e *ToolRefusalError) Error() string {
	return e.Payload
}

// ResourceMeta is what a resource declares beyond its content: who may read
// it. Resources carry no project scope — a reviewed guide is the same document
// for every project in the organization.
type ResourceMeta struct {
	Audiences []Audience
}

func (m ResourceMeta) servesAudience(audience Audience) bool {
	return slices.Contains(m.Audiences, audience)
}

// ResourceDescriptor is one registered resource, reachable either through the
// MCP server's resources/* methods or by direct read.
//
// The direct path exists for the same reason Descriptor's does: a surface that
// speaks Go rather than MCP — the project assistant — must serve the same
// corpus, gated the same way, or a citation link returned by search would
// resolve on one surface and dangle on the other.
type ResourceDescriptor struct {
	URI         string
	Name        string
	Title       string
	Description string
	MIMEType    string
	Meta        ResourceMeta

	read func(ctx context.Context) (string, error)
}

// Read returns the resource's current text. It is a function rather than a
// field because freshness is evaluated per read: a guide that passes its
// revalidation date while the process is running must not keep being served as
// though it were reviewed today.
func (d ResourceDescriptor) Read(ctx context.Context) (string, error) {
	if d.read == nil {
		return "", ErrUnavailable
	}
	return d.read(ctx)
}

// Registrar collects the tools and resources composed for one deployment while
// registering them with the MCP server, so the external endpoint and any other
// admitted audience are built from a single pass rather than two lists that can
// drift.
type Registrar struct {
	server      *mcp.Server
	descriptors []Descriptor
	resources   []ResourceDescriptor
}

func newRegistrar(server *mcp.Server) *Registrar {
	return &Registrar{server: server, descriptors: nil, resources: nil}
}

// Descriptors returns everything registered, before any audience filter.
func (r *Registrar) Descriptors() []Descriptor {
	if r == nil {
		return nil
	}
	return r.descriptors
}

// For returns the descriptors admitted to one audience.
func (r *Registrar) For(audience Audience) []Descriptor {
	if r == nil {
		return nil
	}
	admitted := make([]Descriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		if descriptor.Meta.servesAudience(audience) {
			admitted = append(admitted, descriptor)
		}
	}
	return admitted
}

// ResourceFor returns one admitted resource by URI. An audience that is not
// admitted to a resource cannot tell it apart from one that does not exist.
func (r *Registrar) ResourceFor(audience Audience, uri string) (ResourceDescriptor, bool) {
	if r == nil {
		return ResourceDescriptor{}, false //nolint:exhaustruct // The zero descriptor is the "not found" signal.
	}
	for _, resource := range r.resources {
		if resource.URI == uri && resource.Meta.servesAudience(audience) {
			return resource, true
		}
	}
	return ResourceDescriptor{}, false //nolint:exhaustruct // The zero descriptor is the "not found" signal.
}

// addResource registers one resource with the MCP server and records the
// descriptor that lets an admitted non-MCP audience read the same content.
func addResource(r *Registrar, resource *mcp.Resource, meta ResourceMeta, read func(ctx context.Context) (string, error)) {
	// Registered with the MCP server only when the external endpoint is an
	// admitted audience: the server IS that endpoint, so registering regardless
	// would serve a resource the audience list says it withholds.
	if meta.servesAudience(AudienceExternal) {
		r.server.AddResource(resource, func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			if request.Params.URI != resource.URI {
				return nil, mcp.ResourceNotFoundError(request.Params.URI)
			}
			text, err := read(ctx)
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{ //nolint:exhaustruct // MCP SDK metadata is intentionally omitted.
				Contents: []*mcp.ResourceContents{{
					URI:      resource.URI,
					MIMEType: resource.MIMEType,
					Text:     text,
				}}}, nil
		})
	}

	r.resources = append(r.resources, ResourceDescriptor{
		URI:         resource.URI,
		Name:        resource.Name,
		Title:       resource.Title,
		Description: resource.Description,
		MIMEType:    resource.MIMEType,
		Meta:        meta,
		read:        read,
	})
}

// addTool records the descriptor that lets an admitted audience call this
// handler, and registers the tool with the MCP server when the external
// endpoint is one of those audiences.
//
// The MCP server IS the external surface, so registering unconditionally would
// serve every tool there regardless of what it declared — an audience list
// that reads as a restriction while restricting nothing.
func addTool[In, Out any](r *Registrar, tool *mcp.Tool, meta ToolMeta, handler mcp.ToolHandlerFor[In, Out]) {
	inputSchema, resolved := prepareInputSchema[In](tool)

	if meta.servesAudience(AudienceExternal) {
		// Declared here rather than left to the SDK: the SDK infers the output
		// schema from the Go type alone and cannot see a custom MarshalJSON.
		if tool.OutputSchema == nil {
			tool.OutputSchema = inferOutputSchema[Out](tool.Name)
		}
		mcp.AddTool(r.server, tool, handler)
	}

	r.descriptors = append(r.descriptors, Descriptor{
		Name:        tool.Name,
		Title:       tool.Title,
		Description: tool.Description,
		Annotations: tool.Annotations,
		Meta:        meta,
		InputSchema: inputSchema,
		invoke: func(ctx context.Context, arguments json.RawMessage) (any, error) {
			// The MCP transport applies defaults and validates arguments against
			// the tool's schema before a handler sees them. A direct call has no
			// transport, so it does both here — otherwise the two audiences would
			// pass different inputs to the same handler.
			normalized, err := normalizeAgainstSchema(resolved, arguments)
			if err != nil {
				return nil, fmt.Errorf("validate %s arguments: %w", tool.Name, err)
			}
			var input In
			if len(normalized) > 0 {
				if err := json.Unmarshal(normalized, &input); err != nil {
					return nil, fmt.Errorf("decode %s arguments: %w", tool.Name, err)
				}
			}
			//nolint:exhaustruct // A direct call has no MCP session or params; handlers read the principal from ctx.
			request := &mcp.CallToolRequest{}
			result, output, err := handler(ctx, request, input)
			if err != nil {
				return nil, err
			}
			if refusal, ok := refusalFromResult(result); ok {
				return nil, refusal
			}
			return output, nil
		},
	})
}

// Invoke calls the tool directly. The caller must have bound an authorized
// principal to the context with ContextWithPrincipal.
func (d Descriptor) Invoke(ctx context.Context, arguments json.RawMessage) (any, error) {
	if d.invoke == nil {
		return nil, ErrUnavailable
	}
	return d.invoke(ctx, arguments)
}

// refusalFromResult turns a tool's error result into an error, so a direct
// caller sees the refusal rather than a zero-valued output.
func refusalFromResult(result *mcp.CallToolResult) (*ToolRefusalError, bool) {
	if result == nil || !result.IsError {
		return nil, false
	}
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok && text.Text != "" {
			return &ToolRefusalError{Payload: text.Text}, true
		}
	}
	return &ToolRefusalError{Payload: `{"code":"` + unavailableCode + `"}`}, true
}

// prepareInputSchema returns one schema for all three consumers: MCP transport
// validation, direct invocation validation, and the descriptor advertised to a
// non-MCP audience. A tool-provided schema is authoritative; otherwise the
// schema is inferred from the typed input exactly as before.
func prepareInputSchema[In any](tool *mcp.Tool) ([]byte, *jsonschema.Resolved) {
	source := tool.InputSchema
	if source == nil {
		inferred, err := jsonschema.For[In](nil)
		if err != nil {
			panic(fmt.Sprintf("platformmcp: infer input schema for %q: %v", tool.Name, err))
		}
		source = inferred
	}

	encoded, err := json.Marshal(source)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: encode input schema for %q: %v", tool.Name, err))
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		panic(fmt.Sprintf("platformmcp: decode input schema for %q: %v", tool.Name, err))
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{BaseURI: "", Loader: nil, ValidateDefaults: true})
	if err != nil {
		panic(fmt.Sprintf("platformmcp: resolve input schema for %q: %v", tool.Name, err))
	}
	return encoded, resolved
}

// wireTypeSchemas overrides schema inference for types whose JSON form does not
// match their Go shape. Inference reflects on the Go type and cannot see a
// custom MarshalJSON, so a type that serializes as something other than its
// struct has to say so here.
var wireTypeSchemas = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[SubjectCount](): subjectCountSchema,
}

// inferOutputSchema derives the schema the tool advertises for its result,
// honouring wireTypeSchemas. A tool with an untyped result gets no schema at
// all, which is what the SDK would have done for it.
func inferOutputSchema[Out any](name string) *jsonschema.Schema {
	target := reflect.TypeFor[Out]()
	if target == reflect.TypeFor[any]() {
		return nil
	}
	// Pointer results describe the pointed-to value, matching how the SDK
	// derives the schema it would otherwise have inferred.
	if target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	schema, err := jsonschema.ForType(target, &jsonschema.ForOptions{
		IgnoreInvalidTypes: false,
		TypeSchemas:        wireTypeSchemas,
	})
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer output schema for %q: %v", name, err))
	}
	return schema
}

// normalizeAgainstSchema applies defaults and validation from the tool's
// declared contract to a direct call, then returns the normalized JSON that the
// handler must decode.
func normalizeAgainstSchema(resolved *jsonschema.Resolved, arguments json.RawMessage) (json.RawMessage, error) {
	if resolved == nil {
		return arguments, nil
	}
	var decoded any
	if len(arguments) == 0 {
		decoded = map[string]any{}
	} else if err := json.Unmarshal(arguments, &decoded); err != nil {
		return nil, fmt.Errorf("decode arguments: %w", err)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	if err := resolved.ApplyDefaults(&decoded); err != nil {
		return nil, fmt.Errorf("apply argument defaults: %w", err)
	}
	if err := resolved.Validate(decoded); err != nil {
		return nil, fmt.Errorf("arguments do not match the tool schema: %w", err)
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encode normalized arguments: %w", err)
	}
	return normalized, nil
}

// ErrToolNotFound reports a tool that is not admitted to the requested audience.
var ErrToolNotFound = errors.New("platform mcp tool not found for this audience")
