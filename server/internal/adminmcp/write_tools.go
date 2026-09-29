//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type ProposalIDInput struct {
	ProposalID string `json:"proposal_id" jsonschema:"Opaque proposal ID returned by a prepare tool"`
}

// ProposalOutput is the bounded MCP view of a staff write proposal. Its shape
// is the same for every operation; Preview and Result are operation-specific.
type ProposalOutput struct {
	ProposalID  string          `json:"proposal_id"`
	Status      string          `json:"status"`
	Operation   string          `json:"operation"`
	ExpiresAt   string          `json:"expires_at"`
	Preview     json.RawMessage `json:"preview,omitempty"`
	ApprovalURL string          `json:"approval_url,omitempty"`
	Replay      bool            `json:"replay"`
	ResultCode  string          `json:"result_code,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

func proposalOutput(proposal Proposal, replay bool, baseURL string) ProposalOutput {
	out := ProposalOutput{ProposalID: proposal.ID.String(), Status: string(proposal.Status), Operation: string(proposal.Operation), ExpiresAt: proposal.ExpiresAt.UTC().Format(time.RFC3339), Replay: replay, ResultCode: proposal.ResultCode}
	if proposal.Status == ProposalPendingApproval {
		out.Preview = proposal.Preview
		out.ApprovalURL = baseURL + "/proposals/" + proposal.ID.String()
	}
	if proposal.Status == ProposalSucceeded && len(proposal.ResultPayload) > 0 {
		out.Result = proposal.ResultPayload
	}
	return out
}

// approvableOperation is what browser approval needs from an implemented write.
type approvableOperation interface {
	// revalidate checks the proposal's target and expected state under the
	// approval transaction's locks.
	revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error
	// view turns the stored, digested preview into rows for the approval page.
	view(proposal Proposal) (proposalView, error)
}

// operationWriter is one implemented write. The shared execute and status
// tools dispatch to it by the operation stored on the proposal, never by a
// tool argument.
type operationWriter interface {
	approvableOperation
	execution(authority writeAuthority) ProposalExecution
	// registerPrepare adds the operation's prepare tool.
	registerPrepare(server *mcp.Server)
}

// checkPrepareTarget validates the arguments every prepare tool shares. The
// organization ID is resolved against the database afterwards.
func checkPrepareTarget(organizationID, retryKey string) error {
	if organizationID == "" || len(organizationID) > 128 || organizationID != strings.TrimSpace(organizationID) {
		return errors.New("provide an exact organization ID")
	}
	if retryKey == "" || len(retryKey) > maxIdempotencyKeyLength {
		return errors.New("provide a retry key of at most 128 characters")
	}
	return nil
}

// staffMutation marks ctx as an Admin MCP change made through the caller's
// OAuth client, so audit records the admin_mcp surface that customer feeds
// mask, and returns the staff actor for the domain mutator.
func staffMutation(ctx context.Context, authority writeAuthority) (context.Context, urn.Principal, *string) {
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceAdminMCP))
	ctx = contextvalues.SetOAuthClientID(ctx, authority.Principal.ClientID)
	name := authority.Staff.Name
	if name == "" {
		name = authority.Staff.Email
	}
	return ctx, urn.NewPrincipal(urn.PrincipalTypeUser, authority.Staff.OIDCSubject), &name
}

// proposalView is the readable form of a stored preview on the approval page.
type proposalView struct {
	// Summary is a one-line description of the change.
	Summary string
	// OrganizationID, OrganizationName and OrganizationSlug identify the target.
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
	// Changes lists each setting with its current and proposed value.
	Changes []proposalViewChange
	// SideEffects describes what else the change triggers.
	SideEffects string
}

type proposalViewChange struct {
	Setting string
	Before  string
	After   string
}

// writeTools serves the operation-independent proposal tools.
type writeTools struct {
	store   *proposalStore
	writes  WriteConfig
	baseURL string
	writers map[WriteOperation]operationWriter
}

func newWriteTools(store *proposalStore, writes WriteConfig, baseURL string, writers map[WriteOperation]operationWriter) *writeTools {
	return &writeTools{store: store, writes: writes, baseURL: baseURL, writers: writers}
}

// available reports whether any implemented operation is switched on.
func (t *writeTools) available() bool {
	for op := range t.writers {
		if t.writes.OperationEnabled(op) {
			return true
		}
	}
	return false
}

type ownedProposal struct {
	authority writeAuthority
	owner     proposalOwner
	proposal  Proposal
	writer    operationWriter
}

// load finds the caller's own proposal, then checks write authority for the
// operation stored on it. A proposal with no implemented writer is reported
// as missing.
func (t *writeTools) load(ctx context.Context, input ProposalIDInput) (ownedProposal, error) {
	if !t.available() {
		return ownedProposal{}, ErrWriteDisabled
	}
	identity, err := requireWriteIdentity(ctx)
	if err != nil {
		return ownedProposal{}, err
	}
	owner, err := ownerFromAuthority(identity)
	if err != nil {
		return ownedProposal{}, err
	}
	id, err := uuid.Parse(input.ProposalID)
	if err != nil {
		return ownedProposal{}, ErrProposalNotFound
	}
	proposal, err := t.store.GetForOwner(ctx, id, owner)
	if err != nil {
		return ownedProposal{}, ErrProposalNotFound
	}
	writer := t.writers[proposal.Operation]
	if writer == nil {
		return ownedProposal{}, ErrProposalNotFound
	}
	authority, err := requireWriteAuthority(ctx, t.writes, proposal.Operation)
	if err != nil {
		return ownedProposal{}, err
	}
	return ownedProposal{authority: authority, owner: owner, proposal: proposal, writer: writer}, nil
}

func (t *writeTools) execute(ctx context.Context, input ProposalIDInput) (ProposalOutput, error) {
	loaded, err := t.load(ctx, input)
	if err != nil {
		return ProposalOutput{}, err
	}
	receipt, replay, err := t.store.Execute(ctx, loaded.owner, loaded.proposal.ID, time.Now(), loaded.writer.execution(loaded.authority))
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(receipt, replay, t.baseURL), nil
}

func (t *writeTools) status(ctx context.Context, input ProposalIDInput) (ProposalOutput, error) {
	loaded, err := t.load(ctx, input)
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(loaded.proposal, true, t.baseURL), nil
}

func registerWriteTools(server *mcp.Server, tools *writeTools) {
	if tools == nil || !tools.available() {
		return
	}
	for _, op := range AllWriteOperations {
		if writer := tools.writers[op]; writer != nil && tools.writes.OperationEnabled(op) {
			writer.registerPrepare(server)
		}
	}
	mcp.AddTool(server, &mcp.Tool{Name: "execute_admin_proposal", Title: "Execute Approved Staff Proposal", Description: "Execute an exact stored staff change once, after it was separately approved in the admin site. Takes only its proposal ID, no target or replacement arguments. A committed receipt replays without writing again."}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProposalIDInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := tools.execute(ctx, input)
		return nil, out, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "get_admin_proposal_status", Title: "Get Staff Proposal Status", Description: "Read the bounded status or committed receipt for an exact proposal owned by this staff subject and OAuth client. Does not change or approve a proposal.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProposalIDInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := tools.status(ctx, input)
		return nil, out, err
	})
}

// AttachWrites registers the implemented write operations. Switching on an
// operation that has no implementation is a startup error, so a misconfigured
// server cannot expose an unreviewed write.
func AttachWrites(runtime *Runtime, oauth *StaffOAuth, features *productfeatures.Client, writes WriteConfig) error {
	if runtime == nil || oauth == nil || oauth.Approval == nil || features == nil {
		return errors.New("staff write tools are not configured")
	}
	auditLogger := audit.NewLogger()
	store := oauth.Approval.store
	tools := newWriteTools(store, writes, oauth.Resource(), map[WriteOperation]operationWriter{ //nolint:exhaustive // Only implemented operations are listed.
		OperationSetOrganizationFeature:    &featureWriter{store: store, mutator: productfeatures.NewMutator(features, auditLogger), writes: writes, baseURL: oauth.Resource()},
		OperationSetOrganizationOnboarding: &onboardingWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource()},
	})
	for _, op := range writes.EnabledOperations() {
		if tools.writers[op] == nil {
			return fmt.Errorf("admin MCP write operation %q is not implemented", op)
		}
	}
	approvals := make(map[WriteOperation]approvableOperation, len(tools.writers))
	for op, writer := range tools.writers {
		approvals[op] = writer
	}
	oauth.Approval.operations = approvals
	registerWriteTools(runtime.server, tools)
	return nil
}
