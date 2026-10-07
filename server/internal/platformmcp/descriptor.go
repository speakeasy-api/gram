package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
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
	// ProjectScopeDefaultable: an external caller may name an exact project or
	// omit both selectors to use the organization's literal default project. The
	// assistant still injects its own exact project and hides the selectors.
	ProjectScopeDefaultable
)

// ExternalAuthorization names the call-time authority required by the external
// OAuth surface. It is independent from audience membership: members see one
// stable catalogue, then authorization decides whether each call may run.
type ExternalAuthorization string

const (
	// ExternalAuthorizationMember relies on live membership established while
	// preparing the external request. The handler must enforce any narrower
	// resource, ownership, or domain policy it needs.
	ExternalAuthorizationMember   ExternalAuthorization = "member"
	ExternalAuthorizationOrgAdmin ExternalAuthorization = "org_admin"
)

// ToolMeta is what a tool declares beyond its schemas: who may call it, how it
// obtains its target, and which live policy protects external calls.
type ToolMeta struct {
	Authorization ExternalAuthorization
	Audiences     []Audience
	ProjectScope  ProjectScope
	// DiscoveryScopes are coarse requirements for tools/list. Any listed scope
	// makes the tool discoverable somewhere; handlers still enforce the exact
	// target. An empty list makes a member tool visible to every active member.
	// Organization-admin tools derive org:admin automatically.
	DiscoveryScopes []authz.Scope
}

var (
	discoveryOrgRead             = []authz.Scope{authz.ScopeOrgRead}
	discoveryProjectRead         = []authz.Scope{authz.ScopeProjectRead}
	discoveryProjectWrite        = []authz.Scope{authz.ScopeProjectWrite}
	discoveryMCPRead             = []authz.Scope{authz.ScopeMCPRead}
	discoveryMCPWrite            = []authz.Scope{authz.ScopeMCPWrite}
	discoveryMCPReadOrConnect    = []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPConnect}
	discoveryOrgReadOrMCPConnect = []authz.Scope{authz.ScopeOrgRead, authz.ScopeMCPConnect}
	discoverySkillRead           = []authz.Scope{authz.ScopeSkillRead}
	discoverySkillWrite          = []authz.Scope{authz.ScopeSkillWrite}
)

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

	// output is the handler's result type, the type the advertised output
	// schema is inferred from.
	output reflect.Type
}

// ToolRefusalError is a tool's own refusal — a rate limit, a disabled feature, an
// ineligible target — rather than a failure of the call. The MCP surface
// returns these as an error result; a direct caller receives this error, so
// the reason survives instead of being replaced by an empty payload.
type ToolRefusalError struct {
	Code    string
	Payload string
}

func (e *ToolRefusalError) Error() string {
	return e.Payload
}

// ResourceMeta is what a resource declares beyond its content: who may read it
// and which live policy protects external reads. Resources carry no project
// scope — a reviewed guide is the same document for every project in the
// organization.
type ResourceMeta struct {
	Authorization ExternalAuthorization
	Audiences     []Audience
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
	server             *mcp.Server
	descriptors        []Descriptor
	resources          []ResourceDescriptor
	riskTelemetry      RiskTelemetry
	externalAuthorizer Authorizer
	// logger records the cause of a tool error no classifier recognised. It
	// may be nil — a registrar composed without one still fails safe, it just
	// cannot say why.
	logger *slog.Logger
}

func newRegistrar(server *mcp.Server) *Registrar {
	return &Registrar{server: server, descriptors: nil, resources: nil, riskTelemetry: noopRiskTelemetry{}, externalAuthorizer: nil, logger: nil}
}

func (r *Registrar) withLogger(logger *slog.Logger) {
	if r != nil {
		r.logger = logger
	}
}

func (r *Registrar) withExternalAuthorizer(authorizer Authorizer) {
	if r != nil {
		r.externalAuthorizer = authorizer
	}
}

func (r *Registrar) withRiskTelemetry(telemetry RiskTelemetry) {
	if r != nil && telemetry != nil {
		r.riskTelemetry = telemetry
	}
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

// FilterExternalTools applies caller-specific discovery policy to an already
// composed tools/list result. This is only a discovery filter: exact resource
// checks remain in each handler and run again when a listed tool is called.
func (r *Registrar) FilterExternalTools(ctx context.Context, principal Principal, tools []*mcp.Tool) []*mcp.Tool {
	if r == nil || principal.UserID == "" || principal.OrganizationID == "" {
		return []*mcp.Tool{}
	}
	descriptors := make(map[string]Descriptor, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		if descriptor.Meta.servesAudience(AudienceExternal) {
			descriptors[descriptor.Name] = descriptor
		}
	}
	grants, grantsLoaded := authz.GrantsFromContext(ctx)
	visible := make([]*mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		descriptor, ok := descriptors[tool.Name]
		if !ok || !externalToolDiscoverable(grants, grantsLoaded, principal, descriptor.Meta) {
			continue
		}
		visible = append(visible, tool)
	}
	return visible
}

func externalToolDiscoverable(grants []authz.Grant, grantsLoaded bool, principal Principal, meta ToolMeta) bool {
	if !grantsLoaded {
		return false
	}
	scopes := meta.DiscoveryScopes
	switch meta.Authorization {
	case ExternalAuthorizationMember:
		if len(scopes) == 0 {
			return true
		}
	case ExternalAuthorizationOrgAdmin:
		if !grantsAuthorizeAnyScope(grants, principal.OrganizationID, []authz.Scope{authz.ScopeOrgAdmin}) {
			return false
		}
		if len(scopes) == 0 {
			return true
		}
	default:
		return false
	}
	return grantsAuthorizeAnyScope(grants, principal.OrganizationID, scopes)
}

func grantsAuthorizeAnyScope(grants []authz.Grant, organizationID string, scopes []authz.Scope) bool {
	for _, scope := range scopes {
		if grantsAuthorizeAnyResource(grants, organizationID, scope) {
			return true
		}
	}
	return false
}

func grantsAuthorizeAnyResource(grants []authz.Grant, organizationID string, scope authz.Scope) bool {
	for _, grant := range grants {
		if grant.Scope != authz.ScopeRoot && !slices.Contains(authz.ScopeImplicationClosure(grant.Scope), scope) {
			continue
		}
		resourceID := grant.Selector.ResourceID()
		if scope.Parts().Resource == "org" {
			resourceID = organizationID
		} else if resourceID == "" || resourceID == authz.WildcardResource {
			resourceID = "platform-mcp-catalogue-probe"
		}
		dimensions := maps.Clone(grant.Selector)
		delete(dimensions, authz.SelectorKeyResourceKind)
		delete(dimensions, authz.SelectorKeyResourceID)
		allowed, err := authz.GrantsAuthorize(grants, authz.Check{
			Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions,
		})
		if err == nil && allowed {
			return true
		}
	}
	return false
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
	if meta.servesAudience(AudienceExternal) && meta.Authorization == "" {
		panic(fmt.Sprintf("platformmcp: external resource %q declares no authorization policy", resource.URI))
	}
	// Registered with the MCP server only when the external endpoint is an
	// admitted audience: the server IS that endpoint, so registering regardless
	// would serve a resource the audience list says it withholds.
	if meta.servesAudience(AudienceExternal) {
		r.server.AddResource(resource, func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			if request.Params.URI != resource.URI {
				return nil, mcp.ResourceNotFoundError(request.Params.URI)
			}
			if err := r.authorizeExternal(ctx, meta.Authorization); err != nil {
				return nil, r.failSafe(ctx, resource.URI, err)
			}
			// Reads serve the pinned in-process corpus, and a withheld guide's
			// error is its own answer, so only authorization needs the backstop.
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
	if meta.servesAudience(AudienceExternal) && meta.Authorization == "" {
		panic(fmt.Sprintf("platformmcp: external tool %q declares no authorization policy", tool.Name))
	}

	if meta.servesAudience(AudienceExternal) {
		// Declared here rather than left to the SDK: the SDK infers the output
		// schema from the Go type alone and cannot see a custom MarshalJSON.
		if tool.OutputSchema == nil {
			tool.OutputSchema = inferOutputSchema[Out](tool.Name)
		}
		mcp.AddTool(r.server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
			var result *mcp.CallToolResult
			var output Out
			err := r.authorizeExternal(ctx, meta.Authorization)
			if err == nil {
				result, output, err = handler(ctx, request, input)
			}
			if err == nil {
				return result, output, nil
			}
			// A denial is projected from its fields wherever it was raised, so a
			// handler that wraps one still answers with permission_denied.
			if result, ok := externalAuthorizationToolResult(err); ok {
				return result, output, nil
			}
			return nil, output, r.failSafe(ctx, tool.Name, err)
		})
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
				return nil, r.failSafe(ctx, tool.Name, err)
			}
			if refusal, ok := refusalFromResult(result); ok {
				return nil, refusal
			}
			return output, nil
		},
		output: reflect.TypeFor[Out](),
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

// authorizeExternal admits the caller of one external tool call or resource
// read under the policy it declared.
func (r *Registrar) authorizeExternal(ctx context.Context, policy ExternalAuthorization) error {
	if r.externalAuthorizer == nil {
		return ErrUnavailable
	}
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return err
	}
	if err := r.externalAuthorizer.AuthorizeExternalCall(ctx, principal, policy); err != nil {
		// A denial or bare sentinel is already the answer; only a cause is
		// worth the context.
		if safe, ok := recognisedToolError(err); ok {
			return safe
		}
		return fmt.Errorf("authorize external call: %w", err)
	}
	return nil
}

// failSafe is the backstop for a tool error that no classifier recognised.
//
// Each tool family still projects its own errors into specific refusals —
// those are what make the tools usable — but a projection is only as safe as
// it is exhaustive, across every path including the shared cores a tool calls
// and does not own. Whatever reaches here unrecognised would otherwise be
// returned to the caller as its text, which can carry driver messages, SQL, or
// relation and constraint names. So the caller gets the generic unavailable
// refusal, and the server log keeps the full cause.
//
// It runs on every failure path, including a service that was never composed
// and a registrar given no logger, so the log is guarded rather than assumed.
// Resource authorization goes through it too, named by URI.
func (r *Registrar) failSafe(ctx context.Context, tool string, err error) error {
	if safe, ok := recognisedToolError(err); ok {
		return safe
	}
	if r.logger != nil {
		// A caller that went away or ran out of time is not a server fault.
		level := slog.LevelError
		if ctx.Err() != nil {
			level = slog.LevelWarn
		}
		r.logger.Log(ctx, level, "platform mcp tool returned an unclassified error", attr.SlogToolName(tool), attr.SlogError(err))
	}
	payload, marshalErr := json.Marshal(featureUnavailableResult{
		Code:    unavailableCode,
		Feature: tool,
		Message: "That is temporarily unavailable. Try again shortly, and read the current state again before repeating a change.",
	})
	if marshalErr != nil {
		payload = []byte(`{"code":"` + unavailableCode + `"}`)
	}
	return &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
}

var recognisedSentinels = [...]error{ErrUnauthorized, ErrForbidden, ErrUnavailable}

// recognisedToolError returns the form of err that is already safe to show a
// caller: a tool's own refusal or authorization denial, a protocol error, or
// one of the package's bare sentinels, whose text is fixed. A recognised error
// wrapped in more context is returned unwrapped, because the wrapping text is
// exactly what cannot be vouched for.
func recognisedToolError(err error) (error, bool) {
	for _, sentinel := range recognisedSentinels {
		// Only the bare sentinel: wrapped, it carries its cause's text too.
		if errors.Is(err, sentinel) && err.Error() == sentinel.Error() {
			return sentinel, true
		}
	}
	if refusal, ok := errors.AsType[*ToolRefusalError](err); ok {
		return refusal, true
	}
	if denied, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return denied, true
	}
	if protocol, ok := errors.AsType[*jsonrpc.Error](err); ok {
		return protocol, true
	}
	return nil, false
}

// refusalFromResult turns a tool's error result into an error, so a direct
// caller sees the refusal rather than a zero-valued output.
func refusalFromResult(result *mcp.CallToolResult) (*ToolRefusalError, bool) {
	if result == nil || !result.IsError {
		return nil, false
	}
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok && text.Text != "" {
			return &ToolRefusalError{Code: "", Payload: text.Text}, true
		}
	}
	return &ToolRefusalError{Code: unavailableCode, Payload: `{"code":"` + unavailableCode + `"}`}, true
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

// wireTypeSchemas overrides schema inference for types whose JSON contract is
// narrower than their Go shape. Inference cannot see a custom MarshalJSON or a
// named string's closed vocabulary, so those types declare their wire schema
// here.
var wireTypeSchemas = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[SubjectCount](): subjectCountSchema,
	reflect.TypeFor[SetupCategory](): {
		Type: "string",
		Enum: setupCategoryEnumValues(),
	},
	reflect.TypeFor[MCPBackendKind](): {
		Type: "string",
		Enum: []any{
			string(MCPBackendHosted),
			string(MCPBackendRemote),
			string(MCPBackendTunneled),
			string(MCPBackendUnproxied),
			string(MCPBackendLegacy),
		},
	},
}

// inferOutputSchema derives the schema the tool advertises for its result,
// honouring wireTypeSchemas and leaving every object open to properties it does
// not name. A tool with an untyped result gets no schema at all, which is what
// the SDK would have done for it.
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
	openInferredObjects(schema, map[*jsonschema.Schema]bool{})
	return schema
}

// openInferredObjects drops the `additionalProperties: false` that inference
// puts on every struct-derived object, so a result may carry properties the
// schema does not name.
//
// An output schema is a promise about what a result contains, not a bound on
// what it may contain. A client keeps the schema it fetched when it connected,
// and validates every later result against it; with objects closed, a field
// added to a result after that point fails the whole result, and the client
// reports the tool as broken. Schemas for maps are untouched: their
// additionalProperties is the value schema, not a closure. Input schemas keep
// the closed form, where an unrecognised argument is a caller mistake worth
// rejecting.
func openInferredObjects(schema *jsonschema.Schema, seen map[*jsonschema.Schema]bool) {
	if schema == nil || seen[schema] {
		return
	}
	seen[schema] = true
	if isFalseSchema(schema.AdditionalProperties) {
		schema.AdditionalProperties = nil
	}
	for _, child := range []*jsonschema.Schema{
		schema.AdditionalProperties, schema.Items, schema.AdditionalItems, schema.Contains,
		schema.UnevaluatedItems, schema.PropertyNames, schema.UnevaluatedProperties,
		schema.Not, schema.If, schema.Then, schema.Else, schema.ContentSchema,
	} {
		openInferredObjects(child, seen)
	}
	for _, children := range [][]*jsonschema.Schema{schema.PrefixItems, schema.ItemsArray, schema.AllOf, schema.AnyOf, schema.OneOf} {
		for _, child := range children {
			openInferredObjects(child, seen)
		}
	}
	for _, children := range []map[string]*jsonschema.Schema{
		schema.Defs, schema.Definitions, schema.Properties, schema.PatternProperties,
		schema.DependentSchemas, schema.DependencySchemas,
	} {
		for _, child := range children {
			openInferredObjects(child, seen)
		}
	}
}

// isFalseSchema reports whether schema is the `false` schema, which
// jsonschema-go represents as {"not": {}}.
func isFalseSchema(schema *jsonschema.Schema) bool {
	return schema != nil && schema.Not != nil && reflect.ValueOf(*schema.Not).IsZero()
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
