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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
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
		tools = append(tools, platformtools.ExternalTool{Executor: tool, RequiredFeature: ""})
	}
	return tools
}

// Resource is one admitted resource, readable by the assistant.
type Resource struct {
	descriptor platformmcp.ResourceDescriptor
	authorizer platformmcp.Authorizer
}

// Resources composes the assistant's readable resource set from the catalogue.
//
// The assistant's tool channel has no resources/* methods, so this is how the
// same reviewed corpus reaches it: in process, through the descriptors the
// registrar admitted, rather than by re-serving MCP to ourselves. Anything the
// assistant cites is therefore something it can also open.
func Resources(admitted []platformmcp.ResourceDescriptor, authorizer platformmcp.Authorizer) []Resource {
	resources := make([]Resource, 0, len(admitted))
	for _, descriptor := range admitted {
		resources = append(resources, Resource{descriptor: descriptor, authorizer: authorizer})
	}
	return resources
}

func (r Resource) URI() string   { return r.descriptor.URI }
func (r Resource) Title() string { return r.descriptor.Title }

// Read returns the resource's current content under the assistant's identity.
//
// Authorization is rechecked per read for the same reason Call rechecks it: the
// HTTP handler that authorizes the external surface does not run on this path.
func (r Resource) Read(ctx context.Context) (string, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return "", err
	}
	if r.authorizer == nil {
		return "", platformmcp.ErrUnavailable
	}
	if err := r.authorizer.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return "", fmt.Errorf("authorize assistant platform resource %q: %w", r.descriptor.URI, err)
	}
	text, err := r.descriptor.Read(platformmcp.ContextWithPrincipal(ctx, principal))
	if err != nil {
		return "", fmt.Errorf("read assistant platform resource %q: %w", r.descriptor.URI, err)
	}
	return text, nil
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

	principal, err := principalFromContext(ctx)
	if err != nil {
		return err
	}
	if t.authorizer == nil {
		return platformmcp.ErrUnavailable
	}
	if err := t.authorizer.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return fmt.Errorf("authorize assistant platform tool %q: %w", t.descriptor.Name, err)
	}

	policy, err := targetPolicyFromContext(ctx)
	if err != nil {
		return err
	}
	arguments, err = t.applyTargetPolicy(policy, arguments)
	if err != nil {
		return err
	}

	output, err := t.descriptor.Invoke(platformmcp.ContextWithPrincipal(ctx, principal), arguments)
	if err != nil {
		// A refusal carries its own payload and is returned to the model as
		// the tool's answer; anything else is a failure of this call.
		var refusal *platformmcp.ToolRefusalError
		if errors.As(err, &refusal) {
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
	for _, field := range projectFields(t.descriptor.InputSchema) {
		switch field {
		case "project_slug":
			decoded[field] = policy.ProjectSlug
		case "project_id":
			decoded[field] = policy.ProjectID
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
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"project_slug", "project_id"} {
		delete(properties, field)
	}
	if required, ok := schema["required"].([]any); ok {
		kept := make([]any, 0, len(required))
		for _, value := range required {
			name, _ := value.(string)
			if name == "project_slug" || name == "project_id" {
				continue
			}
			kept = append(kept, value)
		}
		schema["required"] = kept
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return t.descriptor.InputSchema
	}
	return encoded
}

// projectFields reports which project arguments a tool declares, so the policy
// fills the one that tool actually takes.
func projectFields(inputSchema []byte) []string {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		return nil
	}
	fields := make([]string, 0, 2)
	for _, field := range []string{"project_slug", "project_id"} {
		if _, ok := schema.Properties[field]; ok {
			fields = append(fields, field)
		}
	}
	return fields
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

// principalFromContext builds the assistant's identity. The user is the person
// whose turn it is, so authorization and audit attribute to them; there is no
// OAuth connection on this path, and the acting surface says so.
func principalFromContext(ctx context.Context) (platformmcp.Principal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.UserID == "" {
		return platformmcp.Principal{}, fmt.Errorf("assistant platform tools require user and organization auth context")
	}
	return platformmcp.Principal{
		UserID:         authCtx.UserID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       platformmcp.AssistantClientID,
		Surface:        platformmcp.SurfaceProjectAssistant,
	}, nil
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
