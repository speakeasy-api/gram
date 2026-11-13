// Package assistant_platform_mcp_adapter serves the Platform MCP capability
// catalogue to a project's managed assistant.
//
// It is the assistant-owned side of the catalogue, not a second copy of it.
// Descriptors are declared once in platformmcp; this package composes the ones
// admitted to the assistant audience, injects the assistant's own target
// policy, and calls their handlers directly. It never reaches the external
// /platform-mcp route and never uses an external OAuth connection: the
// assistant acts under assistant identity, which is what the audit trail
// records.
//
// Composing rather than re-serving matters for three reasons. Audience
// membership becomes a per-tool decision that a reviewer can see, instead of
// every Platform MCP tool reaching the assistant the moment it is added.
// Target policy is injected here, so a model is never asked which project to
// act in when the assistant only ever acts in its own. And a refusal keeps its
// reason, because there is no second protocol hop to flatten it through.
package assistant_platform_mcp_adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TargetPolicy is the scope one call acts in. A managed assistant is
// provisioned per project and only ever acts in its own, so the project comes
// from the calling assistant's auth context rather than from the model.
//
// It is resolved per call, not per process: one composed toolset serves every
// project's assistant, so binding a project at startup would attribute every
// call to whichever project happened to start first.
type TargetPolicy struct {
	ProjectID   string
	ProjectSlug string
}

// Tool is one admitted descriptor, callable by the assistant.
type Tool struct {
	descriptor platformmcp.Descriptor
	authorizer platformmcp.Authorizer
}

// Tools composes the assistant's toolset from the catalogue. Only descriptors
// admitted to the assistant audience are returned; everything else in the
// catalogue stays external-only.
func Tools(admitted []platformmcp.Descriptor, authorizer platformmcp.Authorizer) []Tool {
	tools := make([]Tool, 0, len(admitted))
	for _, descriptor := range admitted {
		tools = append(tools, Tool{descriptor: descriptor, authorizer: authorizer})
	}
	return tools
}

// ExternalTools adapts the composed set to the platform tool channel.
func ExternalTools(admitted []platformmcp.Descriptor, authorizer platformmcp.Authorizer) []platformtools.ExternalTool {
	composed := Tools(admitted, authorizer)
	tools := make([]platformtools.ExternalTool, 0, len(composed))
	for _, tool := range composed {
		tools = append(tools, platformtools.ExternalTool{Executor: tool, RequiredFeature: "", RequiredScope: assistantRequiredScope(tool.descriptor)})
	}
	return tools
}

// assistantRequiredScope hides tools that list individual tool calls from an
// assistant turn without log:read on its project. Every call to them would be
// refused, and log:read is held by no system role and no agent runtime policy.
// Other discovery scopes are coarse "held somewhere" hints, so they are not
// carried here.
func assistantRequiredScope(descriptor platformmcp.Descriptor) string {
	if slices.Contains(descriptor.Meta.DiscoveryScopes, authz.ScopeLogRead) {
		return string(authz.ScopeLogRead)
	}
	return ""
}

func (t Tool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  platformtools.SourcePlatform,
		HandlerName: t.descriptor.Name,
		Name:        t.descriptor.Name,
		Description: t.descriptor.Description,
		InputSchema: t.assistantInputSchema(),
		Variables:   nil,
		Annotations: annotations(t.descriptor.Annotations),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

// Call executes the tool under the assistant's identity.
//
// Authorization is rechecked here on every call. The external surface performs
// it in its HTTP handler, which does not run on this path, and the RFC
// requires membership, grants, and target ownership to be live rather than
// carried from an earlier decision.
func (t Tool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	arguments, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read platform tool arguments: %w", err)
	}

	ctx, acting, err := actAs(ctx, t.authorizer)
	if errors.Is(err, errPersonRequired) {
		if _, writeErr := wr.Write(personRequiredRefusal); writeErr != nil {
			return fmt.Errorf("write platform tool refusal: %w", writeErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("prepare assistant platform tool %q: %w", t.descriptor.Name, err)
	}
	principal := acting.principal

	arguments, err = t.applyTargetPolicy(acting.policy, arguments)
	if err != nil {
		return err
	}
	if err := t.authorizer.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return fmt.Errorf("authorize assistant platform tool %q: %w", t.descriptor.Name, err)
	}

	output, err := t.descriptor.Invoke(platformmcp.ContextWithPrincipal(ctx, principal), arguments)
	if err != nil {
		// A refusal carries its own payload and is returned to the model as
		// the tool's answer; anything else is a failure of this call.
		if refusal, ok := errors.AsType[*platformmcp.ToolRefusalError](err); ok {
			if _, writeErr := wr.Write([]byte(refusal.Payload)); writeErr != nil {
				return fmt.Errorf("write platform tool refusal: %w", writeErr)
			}
			return nil
		}
		return fmt.Errorf("call assistant platform tool %q: %w", t.descriptor.Name, err)
	}

	encoded, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("encode platform tool result %q: %w", t.descriptor.Name, err)
	}
	if _, err := wr.Write(encoded); err != nil {
		return fmt.Errorf("write platform tool result: %w", err)
	}
	return nil
}

// applyTargetPolicy fills the project the assistant acts in. The value always
// wins over anything the model supplied: an assistant that reaches for another
// project is not expressing intent the policy should honour.
func (t Tool) applyTargetPolicy(policy TargetPolicy, arguments []byte) ([]byte, error) {
	if t.descriptor.Meta.ProjectScope == platformmcp.ProjectScopeNone {
		return arguments, nil
	}

	// A model can send `null` or a non-object, which decodes to a nil map;
	// assigning the policy into that would panic and take the request down
	// rather than failing the one call.
	decoded := map[string]any{}
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &decoded); err != nil {
			return nil, fmt.Errorf("decode platform tool arguments %q: %w", t.descriptor.Name, err)
		}
		if decoded == nil {
			decoded = map[string]any{}
		}
	}
	fields := projectFields(t.descriptor.InputSchema)
	// Prefer the immutable project ID when a descriptor permits either selector.
	// Supplying both would turn an externally valid optional-selector schema into
	// an invalid assistant call; the assistant has one exact project to inject.
	for _, field := range fields {
		if field == "project_id" {
			decoded["project_id"] = policy.ProjectID
			delete(decoded, "project_slug")
			break
		}
		if field == "project_slug" {
			decoded["project_slug"] = policy.ProjectSlug
		}
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encode platform tool arguments %q: %w", t.descriptor.Name, err)
	}
	return encoded, nil
}

// assistantInputSchema hides the project fields the policy supplies, so the
// model is neither asked for a value it cannot choose nor able to name a
// project outside the assistant's own.
func (t Tool) assistantInputSchema() []byte {
	if t.descriptor.Meta.ProjectScope == platformmcp.ProjectScopeNone {
		return t.descriptor.InputSchema
	}

	var schema map[string]any
	if err := json.Unmarshal(t.descriptor.InputSchema, &schema); err != nil {
		return t.descriptor.InputSchema
	}
	hideProjectFields(schema)
	encoded, err := json.Marshal(schema)
	if err != nil {
		return t.descriptor.InputSchema
	}
	return encoded
}

func hideProjectFields(schema map[string]any) {
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"project_slug", "project_id"} {
		delete(properties, field)
	}
	if required, ok := schema["required"].([]any); ok {
		kept := make([]any, 0, len(required))
		for _, value := range required {
			name, _ := value.(string)
			if name != "project_slug" && name != "project_id" {
				kept = append(kept, value)
			}
		}
		schema["required"] = kept
	}
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			if nested, ok := branch.(map[string]any); ok {
				hideProjectFields(nested)
			}
		}
	}
	if nested, ok := schema["not"].(map[string]any); ok {
		projectConstraint := schemaReferencesProjectFields(nested)
		hideProjectFields(nested)
		if projectConstraint && !schemaHasConstraints(nested) {
			delete(schema, "not")
		}
	}
}

func schemaReferencesProjectFields(schema map[string]any) bool {
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"project_slug", "project_id"} {
		if _, ok := properties[field]; ok {
			return true
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, value := range required {
			name, _ := value.(string)
			if name == "project_slug" || name == "project_id" {
				return true
			}
		}
	}
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			if nested, ok := branch.(map[string]any); ok && schemaReferencesProjectFields(nested) {
				return true
			}
		}
	}
	if nested, ok := schema["not"].(map[string]any); ok {
		return schemaReferencesProjectFields(nested)
	}
	return false
}

func schemaHasConstraints(schema map[string]any) bool {
	for keyword, value := range schema {
		switch typed := value.(type) {
		case []any:
			if len(typed) > 0 {
				return true
			}
		case map[string]any:
			if len(typed) > 0 {
				return true
			}
		case nil:
			continue
		default:
			if keyword != "required" || typed != nil {
				return true
			}
		}
	}
	return false
}

// projectFields reports which project arguments a tool declares, including in
// discriminated schema branches, so the policy fills the one it actually takes.
func projectFields(inputSchema []byte) []string {
	var schema map[string]any
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		return nil
	}
	found := map[string]bool{}
	collectProjectFields(schema, found)
	fields := make([]string, 0, 2)
	for _, field := range []string{"project_slug", "project_id"} {
		if found[field] {
			fields = append(fields, field)
		}
	}
	return fields
}

func collectProjectFields(schema map[string]any, found map[string]bool) {
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"project_slug", "project_id"} {
		if _, ok := properties[field]; ok {
			found[field] = true
		}
	}
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			if nested, ok := branch.(map[string]any); ok {
				collectProjectFields(nested, found)
			}
		}
	}
}

// targetPolicyFromContext reads the project the calling assistant belongs to.
// A turn always carries one; without it there is nothing to scope the call to,
// and guessing would act on the wrong project.
func targetPolicyFromContext(ctx context.Context) (TargetPolicy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return TargetPolicy{}, fmt.Errorf("assistant platform tools require a project auth context")
	}
	policy := TargetPolicy{ProjectID: authCtx.ProjectID.String(), ProjectSlug: ""}
	if authCtx.ProjectSlug != nil {
		policy.ProjectSlug = *authCtx.ProjectSlug
	}
	return policy, nil
}

// acting is who a platform tool call acts for and the project it acts in.
type acting struct {
	principal platformmcp.Principal
	policy    TargetPolicy
}

// actingKey marks a context that actAs has already prepared, so a request
// derives its acting context once and every later step reuses it.
type actingKey struct{}

// ActingContext returns the context the Project Assistant's platform toolset
// lists and calls its tools in, so what a turn is offered matches what it may
// call. A turn that acts for no person keeps its own context; its calls are
// refused with person_required.
func ActingContext(authorizer platformmcp.Authorizer) func(ctx context.Context) (context.Context, error) {
	return func(ctx context.Context) (context.Context, error) {
		prepared, _, err := actAs(ctx, authorizer)
		if errors.Is(err, errPersonRequired) {
			return ctx, nil
		}
		return prepared, err
	}
}

// actAs resolves who a platform tool call acts for and the context it runs in.
//
// An agent-backed turn acts for the person who vouched for it, such as the
// dashboard user who sent the message. Platform tools need organization-wide
// authority, which an agent's own policy never holds, so these tools run
// exactly as if that person called them: their auth context and their live
// grants, with the agent's admitted policy set aside. The assistant principal
// and turn credential stay on the context, so writes are still attributed to
// the Project Assistant surface, and the assistant's tool-call audit entry
// records the agent acting for this person.
func actAs(ctx context.Context, authorizer platformmcp.Authorizer) (context.Context, acting, error) {
	if prepared, ok := ctx.Value(actingKey{}).(acting); ok {
		return ctx, prepared, nil
	}
	principal, delegated, err := principalFromContext(ctx)
	if err != nil {
		return ctx, acting{}, err
	}
	if authorizer == nil {
		return ctx, acting{}, platformmcp.ErrUnavailable
	}
	// The project comes from the assistant's own context, before a delegated
	// context replaces it.
	policy, err := targetPolicyFromContext(ctx)
	if err != nil {
		return ctx, acting{}, err
	}
	if delegated {
		assistantCtx, _ := contextvalues.GetAuthContext(ctx)
		ctx, err = authorizer.PrepareExternalContext(ctx, principal)
		if err != nil {
			return ctx, acting{}, fmt.Errorf("act for the assistant turn's person: %w", err)
		}
		ctx = authz.WithoutAdmittedPolicies(ctx)

		// The person acts in the assistant's project, which the platform
		// toolset reads from the auth context.
		personCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || personCtx == nil {
			return ctx, acting{}, fmt.Errorf("act for the assistant turn's person: no auth context")
		}
		inProject := *personCtx
		inProject.ProjectID = assistantCtx.ProjectID
		inProject.ProjectSlug = assistantCtx.ProjectSlug
		ctx = contextvalues.SetAuthContext(ctx, &inProject)
	}
	prepared := acting{principal: principal, policy: policy}
	return context.WithValue(ctx, actingKey{}, prepared), prepared, nil
}

// errPersonRequired means the turn acts for no person, such as a workload
// running a scheduled trigger. Platform tools authorize and attribute every call
// to a person, so these turns cannot use them.
var errPersonRequired = errors.New("assistant platform tools require a person to act for")

// personRequiredRefusal is returned to the model instead of an error, so the
// runner does not treat a turn that can never use these tools as a dropped
// connection and retry.
var personRequiredRefusal = []byte(`{"code":"person_required","message":"Platform tools act for a person, and this assistant turn does not act for anyone. Ask a person to run this request from the dashboard."}`)

// principalFromContext builds the assistant's identity. The user is the person
// whose turn it is, so authorization and audit attribute to them; there is no
// OAuth connection on this path, and the acting surface says so.
//
// A turn authenticated with an assistant turn credential carries no user on
// its auth context. When the credential's agent acts for a person, that person
// is the user and delegated is true; when the credential is a workload's, there
// is no person and the error is errPersonRequired.
func principalFromContext(ctx context.Context) (principal platformmcp.Principal, delegated bool, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return platformmcp.Principal{}, false, fmt.Errorf("assistant platform tools require an organization auth context")
	}

	userID := authCtx.UserID
	if credential, ok := principalcredential.FromContext(ctx); ok {
		if credential.Credential.Principal.Type != urn.PrincipalTypeAgent || credential.Credential.AuthorizerUserID == "" {
			return platformmcp.Principal{}, false, errPersonRequired
		}
		if credential.Credential.OrganizationID != authCtx.ActiveOrganizationID {
			return platformmcp.Principal{}, false, fmt.Errorf("assistant turn credential belongs to another organization")
		}
		if _, isAssistant := contextvalues.GetAssistantPrincipal(ctx); !isAssistant {
			return platformmcp.Principal{}, false, fmt.Errorf("assistant platform tools require an assistant turn")
		}
		userID = credential.Credential.AuthorizerUserID
		delegated = true
	}
	if userID == "" {
		return platformmcp.Principal{}, false, errPersonRequired
	}

	return platformmcp.Principal{
		UserID:         userID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       platformmcp.AssistantClientID,
		Surface:        platformmcp.SurfaceProjectAssistant,
	}, delegated, nil
}

func annotations(source *mcp.ToolAnnotations) *types.ToolAnnotations {
	if source == nil {
		return nil
	}
	readOnly := source.ReadOnlyHint
	idempotent := source.IdempotentHint
	destructive := false
	if source.DestructiveHint != nil {
		destructive = *source.DestructiveHint
	}
	openWorld := false
	if source.OpenWorldHint != nil {
		openWorld = *source.OpenWorldHint
	}
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
