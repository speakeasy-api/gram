package declarative

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/declarative/relschema"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// ChatRef is the trusted identity and attribution of one session, built by
// the chat service from its tenant-scoped row. Attribution is what capture
// recorded; nothing here is inferred.
type ChatRef struct {
	ID             string
	ProjectID      string
	OrganizationID string
	// AuthorUserID is the member the session is attributed to, if any.
	AuthorUserID string
	// ExternalUserID is a non-member human identity, such as an email.
	ExternalUserID string
	// AgentID is the agent principal that produced the session, if any.
	AgentID string
}

// AgentOwnerFact is the current ownership of the agent a session names, as
// the service loaded it from the agents row in the session's organization.
type AgentOwnerFact struct {
	AgentID        string
	OrganizationID string
	OwnerUserID    string
	// Eligible is false when the agent is deleted or latched for owner
	// reassignment: its owner then answers for nothing.
	Eligible bool
}

// ChatDecision is the answer for reading one session, with its trace.
type ChatDecision struct {
	Object   string
	Allowed  bool
	Policies []MCPServerPolicyOutcome
}

var errChatRefInvalid = errors.New("chat reference is incomplete")

// ChatSchemaActive reports whether the caller's organization reads sessions
// through the schema. It is the same switch as MCP servers.
func ChatSchemaActive(ctx context.Context, engine *authz.Engine) (bool, error) {
	active, err := engine.MCPServerSchemaActive(ctx)
	if err != nil {
		return false, engine.MapError(ctx, err)
	}
	return active, nil
}

// RequireChatRead enforces reading chat for the caller. agent is the
// ownership of chat.AgentID, or nil when the session names no agent or the
// agent row was not found. Callers check ChatSchemaActive first.
func RequireChatRead(ctx context.Context, engine *authz.Engine, chat ChatRef, agent *AgentOwnerFact) error {
	legacy := authz.ChatReadCheck(chat.ID)
	set, err := engine.PolicySet(ctx)
	if err != nil {
		return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: false, DenyReason: "", Trace: "", Err: err}) //nolint:wrapcheck // authz errors carry their response code.
	}
	if !set.Enforced {
		return nil
	}
	decision, err := decideChatRead(set, callerSubject(ctx), chat, agent)
	if err != nil {
		return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: false, DenyReason: "", Trace: "", Err: err}) //nolint:wrapcheck // authz errors carry their response code.
	}
	asServer := MCPServerDecision{Permission: "", Object: decision.Object, OrganizationID: set.OrganizationID, Allowed: decision.Allowed, Policies: decision.Policies}
	return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: decision.Allowed, DenyReason: asServer.denyReason(), Trace: asServer.trace(), Err: nil}) //nolint:wrapcheck // authz errors carry their response code.
}

// ChatListScope is how a session listing must be filtered so that it returns
// exactly the sessions RequireChatRead allows. All means every session in the
// project; otherwise the listing keeps the caller's own sessions and the
// agent-only sessions of the agents they currently own.
type ChatListScope struct {
	All bool
}

// PlanChatList derives the listing filter from the schema: a session with no
// attribution is readable only through grants, so if one is readable, every
// session in the project is. The owner branches are what the listing SQL
// expresses for the rest.
func PlanChatList(ctx context.Context, engine *authz.Engine, projectID, organizationID string) (ChatListScope, error) {
	set, err := engine.PolicySet(ctx)
	if err != nil {
		return ChatListScope{}, engine.MapError(ctx, err)
	}
	if !set.Enforced {
		return ChatListScope{All: true}, nil
	}
	unattributed := ChatRef{ID: "any", ProjectID: projectID, OrganizationID: organizationID, AuthorUserID: "", ExternalUserID: "", AgentID: ""}
	decision, err := decideChatRead(set, relschema.ObjectRef{}, unattributed, nil)
	if err != nil {
		return ChatListScope{}, engine.MapError(ctx, err)
	}
	return ChatListScope{All: decision.Allowed}, nil
}

// decideChatRead evaluates reading chat for every policy in set and allows
// only if all allow. It performs no I/O.
func decideChatRead(set authz.PolicySet, subject relschema.ObjectRef, chat ChatRef, agent *AgentOwnerFact) (ChatDecision, error) {
	decision := ChatDecision{Object: typeChat + ":" + chat.ID, Allowed: false, Policies: nil}
	if chat.ID == "" || chat.ProjectID == "" || chat.OrganizationID == "" {
		return decision, errChatRefInvalid
	}
	if set.OrganizationID == "" {
		decision.Policies = []MCPServerPolicyOutcome{{Outcome: relschema.Undetermined.String(), Reason: "policies are not bound to the request's organization", Path: nil}}
		return decision, nil
	}

	object, request := chatObject(subject, chat, agent)
	allowed, policies, err := evaluatePoliciesWith(set, object, string(ChatRead), request)
	decision.Allowed, decision.Policies = allowed, policies
	return decision, err
}

// chatObject turns the service's facts into the schema's object and request.
// The owner attribute is set only for an eligible agent; an agent row that
// was not loaded is left out of the request, so traversal to it is
// undetermined and never allows.
func chatObject(subject relschema.ObjectRef, chat ChatRef, agent *AgentOwnerFact) (relschema.Object, relschema.Request) {
	attrs := map[string]relschema.ObjectRef{}
	if chat.AuthorUserID != "" {
		attrs["author"] = relschema.ObjectRef{Type: typeUser, ID: chat.AuthorUserID}
	}
	if chat.ExternalUserID != "" {
		attrs["external_author"] = relschema.ObjectRef{Type: typeUser, ID: "external:" + chat.ExternalUserID}
	}
	request := relschema.Request{Facts: nil, Subject: subject, Objects: map[relschema.ObjectRef]relschema.Object{}}
	if chat.AgentID != "" {
		ref := relschema.ObjectRef{Type: typeAgent, ID: chat.AgentID}
		attrs["agent"] = ref
		if agent != nil && agent.AgentID == chat.AgentID {
			agentAttrs := map[string]relschema.ObjectRef{}
			if agent.Eligible && agent.OwnerUserID != "" {
				agentAttrs["owner"] = relschema.ObjectRef{Type: typeUser, ID: agent.OwnerUserID}
			}
			request.Objects[ref] = relschema.Object{
				Type:      typeAgent,
				ID:        agent.AgentID,
				Ancestors: []relschema.ObjectRef{{Type: typeOrganization, ID: agent.OrganizationID}},
				Attrs:     agentAttrs,
			}
		}
	}
	object := relschema.Object{
		Type: typeChat,
		ID:   chat.ID,
		Ancestors: []relschema.ObjectRef{
			{Type: typeProject, ID: chat.ProjectID},
			{Type: typeOrganization, ID: chat.OrganizationID},
		},
		Attrs: attrs,
	}
	return object, request
}

// callerSubject is the caller as a person, or empty when no member is
// authenticated.
func callerSubject(ctx context.Context) relschema.ObjectRef {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return relschema.ObjectRef{}
	}
	return relschema.ObjectRef{Type: typeUser, ID: authCtx.UserID}
}

func (x ChatDecision) String() string {
	return fmt.Sprintf("%s read allowed=%t %v", x.Object, x.Allowed, x.Policies)
}

// AuditGrants returns one error for every stored row the schema reads but
// cannot express. Any such row makes every schema check in its policy fail
// closed, so an organization is switched on only once this is empty; the
// errors name each row so it can be corrected or removed.
func AuditGrants(organizationID string, grants []authz.Grant) []error {
	var problems []error
	for i := range grants {
		if _, err := decodeMCPServerPolicy(organizationID, grants[i:i+1]); err != nil {
			problems = append(problems, err)
		}
	}
	return problems
}
