package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// ResourcesFor returns the resource descriptors admitted to one audience.
func (r *Registrar) ResourcesFor(audience Audience) []ResourceDescriptor {
	if r == nil {
		return nil
	}
	admitted := make([]ResourceDescriptor, 0, len(r.resources))
	for _, resource := range r.resources {
		if resource.Meta.servesAudience(audience) {
			admitted = append(admitted, resource)
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
	if !meta.servesAudience(AudienceExternal) {
		r.resources = append(r.resources, ResourceDescriptor{
			URI:         resource.URI,
			Name:        resource.Name,
			Title:       resource.Title,
			Description: resource.Description,
			MIMEType:    resource.MIMEType,
			Meta:        meta,
			read:        read,
		})
		return
	}

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
	if meta.servesAudience(AudienceExternal) {
		mcp.AddTool(r.server, tool, handler)
	}

	resolved := resolveInputSchema[In](tool.Name)

	r.descriptors = append(r.descriptors, Descriptor{
		Name:        tool.Name,
		Title:       tool.Title,
		Description: tool.Description,
		Annotations: tool.Annotations,
		Meta:        meta,
		InputSchema: inferInputSchema[In](tool.Name),
		invoke: func(ctx context.Context, arguments json.RawMessage) (any, error) {
			// The MCP transport validates arguments against the tool's schema
			// before a handler sees them. A direct call has no transport, so
			// it validates here — otherwise the two audiences would enforce
			// different contracts for the same tool.
			if err := validateAgainstSchema(resolved, arguments); err != nil {
				return nil, fmt.Errorf("validate %s arguments: %w", tool.Name, err)
			}
			var input In
			if len(arguments) > 0 {
				if err := json.Unmarshal(arguments, &input); err != nil {
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

// resolveInputSchema compiles the tool's schema once, so a direct call can
// validate arguments the way the MCP transport does.
func resolveInputSchema[In any](name string) *jsonschema.Resolved {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer input schema for %q: %v", name, err))
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: resolve input schema for %q: %v", name, err))
	}
	return resolved
}

// validateAgainstSchema applies the tool's declared contract to a direct call.
func validateAgainstSchema(resolved *jsonschema.Resolved, arguments json.RawMessage) error {
	if resolved == nil {
		return nil
	}
	var decoded any
	if len(arguments) == 0 {
		decoded = map[string]any{}
	} else if err := json.Unmarshal(arguments, &decoded); err != nil {
		return fmt.Errorf("decode arguments: %w", err)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	if err := resolved.Validate(decoded); err != nil {
		return fmt.Errorf("arguments do not match the tool schema: %w", err)
	}
	return nil
}

// inferInputSchema derives the JSON Schema a non-MCP surface advertises. The
// MCP server infers its own from the same type, so the two always agree.
func inferInputSchema[In any](name string) []byte {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: infer input schema for %q: %v", name, err))
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("platformmcp: encode input schema for %q: %v", name, err))
	}
	return encoded
}

// ErrToolNotFound reports a tool that is not admitted to the requested audience.
var ErrToolNotFound = errors.New("platform mcp tool not found for this audience")
