//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
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

// proposalOutputSchema is ProposalOutput's schema with Preview and Result typed
// as JSON objects. Inferred from the Go type, json.RawMessage reads as []byte,
// and the SDK then rejects every object preview after the proposal is already
// stored. An empty schema is no better: it serializes as the boolean schema
// true, which MCP clients reject as a property schema. Every preview and
// receipt is an encoded struct, so "object" is exact.
var proposalOutputSchema = func() *jsonschema.Schema {
	schema, err := jsonschema.For[ProposalOutput](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): {Type: "object"}}})
	if err != nil {
		panic(fmt.Sprintf("infer proposal output schema: %v", err))
	}
	return schema
}()

// addProposalTool registers a tool that returns ProposalOutput.
func addProposalTool[In any](server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, ProposalOutput]) {
	tool.OutputSchema = proposalOutputSchema
	mcp.AddTool(server, tool, handler)
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
	Summary        string
	PlatformGlobal bool
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
	// uploads backs the direct upload_platform_image write; nil without asset storage.
	uploads PlatformImageUploader
}

// implements reports whether op has a shipped implementation on this server.
func (t *writeTools) implements(op WriteOperation) bool {
	if op == OperationUploadPlatformImage {
		return t.uploads != nil
	}
	return t.writers[op] != nil
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
	addProposalTool(server, &mcp.Tool{Name: "execute_admin_proposal", Title: "Execute Approved Staff Proposal", Description: "Execute an exact stored staff change once, after it was separately approved in the admin site. Takes only its proposal ID, no target or replacement arguments. A committed receipt replays without writing again."}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProposalIDInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := tools.execute(ctx, input)
		return nil, out, err
	})
	addProposalTool(server, &mcp.Tool{Name: "get_admin_proposal_status", Title: "Get Staff Proposal Status", Description: "Read the bounded status or committed receipt for an exact proposal owned by this staff subject and OAuth client. Does not change or approve a proposal.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProposalIDInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := tools.status(ctx, input)
		return nil, out, err
	})
}

// WriteDependencies are the domain services that implemented writes call.
type WriteDependencies struct {
	Features *productfeatures.Client
	Registry *mcpregistry.Service
	// ImageUploads is nil when the admin server has no asset storage, which
	// leaves upload_platform_image unimplemented.
	ImageUploads PlatformImageUploader
}

// AttachWrites registers the implemented write operations. Switching on an
// operation that has no implementation is a startup error, so a misconfigured
// server cannot expose an unreviewed write.
func AttachWrites(runtime *Runtime, oauth *StaffOAuth, deps WriteDependencies, writes WriteConfig) error {
	if runtime == nil || oauth == nil || oauth.Approval == nil || deps.Features == nil || deps.Registry == nil {
		return errors.New("staff write tools are not configured")
	}
	features, registry := deps.Features, deps.Registry
	auditLogger := audit.NewLogger()
	store := oauth.Approval.store
	tools := newWriteTools(store, writes, oauth.Resource(), map[WriteOperation]operationWriter{ //nolint:exhaustive // Only implemented operations are listed.
		OperationSetOrganizationFeature:               &featureWriter{store: store, mutator: productfeatures.NewMutator(features, auditLogger), writes: writes, baseURL: oauth.Resource()},
		OperationAssignOrganizationOnboardingPlaybook: &onboardingPlaybookWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource()},
		OperationSetOrganizationWhitelist:             &organizationWhitelistWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource(), now: time.Now},
		OperationExtendOrganizationTrial:              &trialWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource()},
		OperationSetChatAnalysisSettings:              &chatAnalysisWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource(), now: time.Now},
		OperationEnableOrganization:                   &organizationAccessWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource(), operation: OperationEnableOrganization},
		OperationDisableOrganization:                  &organizationAccessWriter{store: store, audit: auditLogger, writes: writes, baseURL: oauth.Resource(), operation: OperationDisableOrganization},
		OperationUpdateSupportMatrix:                  &supportMatrixWriter{store: store, writes: writes, baseURL: oauth.Resource()},
		OperationCreateRegistryEntry:                  &registryEntryWriter{store: store, registry: registry, writes: writes, baseURL: oauth.Resource()},
		OperationSetRegistryIcons:                     &registryIconsWriter{store: store, registry: registry, writes: writes, baseURL: oauth.Resource()},
	})
	tools.uploads = deps.ImageUploads
	if runtime.catalogPromotions != nil {
		tools.writers[OperationCurateRegistryPromotionCandidate] = &catalogCurationWriter{store: store, writes: writes, baseURL: oauth.Resource(), mutator: runtime.catalogPromotions, now: time.Now}
		tools.writers[OperationRetryRegistryPromotionDocs] = &catalogDocsRetryWriter{store: store, writes: writes, baseURL: oauth.Resource(), mutator: runtime.catalogPromotions, now: time.Now}
		tools.writers[OperationPromoteRegistryCandidate] = &catalogPromotionWriter{store: store, writes: writes, baseURL: oauth.Resource(), mutator: runtime.catalogPromotions, now: time.Now}
	}
	for _, op := range writes.EnabledOperations() {
		if !tools.implements(op) {
			return fmt.Errorf("admin MCP write operation %q is not implemented", op)
		}
	}
	approvals := make(map[WriteOperation]approvableOperation, len(tools.writers))
	for op, writer := range tools.writers {
		approvals[op] = writer
	}
	oauth.Approval.operations = approvals
	runtime.writes = tools
	registerWriteTools(runtime.server, tools)
	if tools.uploads != nil && writes.OperationEnabled(OperationUploadPlatformImage) {
		registerImageUploadTool(runtime.server, tools.uploads, writes)
	}
	return nil
}
