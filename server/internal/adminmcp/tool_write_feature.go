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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The first write slice deliberately supports only the reversible logs flag.
// Additional features require their own side-effect review before joining this list.
const writableFeature = productfeatures.FeatureLogs

type PrepareFeatureInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	Feature        string `json:"feature" jsonschema:"Feature to change (currently logs only)"`
	Enabled        bool   `json:"enabled" jsonschema:"Desired enabled state"`
	RetryKey       string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type ProposalIDInput struct {
	ProposalID string `json:"proposal_id" jsonschema:"Opaque ID from prepare_set_organization_feature"`
}

type FeatureProposalOutput struct {
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

type featureChange struct {
	Feature string `json:"feature"`
	Enabled bool   `json:"enabled"`
}

type featureState struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Enabled        bool   `json:"enabled"`
}

type featurePreview struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Feature        string `json:"feature"`
	Before         bool   `json:"before"`
	After          bool   `json:"after"`
	SideEffects    string `json:"side_effects"`
}

type featureWriter struct {
	store   *proposalStore
	mutator *productfeatures.Mutator
	writes  WriteConfig
	baseURL string
}

func (f *featureWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string) (featureState, error) {
	id, err := featurerepo.New(tx).LockOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && id != organizationID) {
		return featureState{}, ErrStaleState
	}
	if err != nil {
		return featureState{}, fmt.Errorf("lock feature target: %w", err)
	}
	org, err := orgrepo.New(tx).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return featureState{}, fmt.Errorf("read feature target: %w", err)
	}
	enabled, err := featurerepo.New(tx).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(writableFeature)})
	if err != nil {
		return featureState{}, fmt.Errorf("read feature state: %w", err)
	}
	return featureState{OrganizationID: id, Name: org.Name, Slug: org.Slug, Enabled: enabled}, nil
}

func (f *featureWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (featureState, featureChange, error) {
	if proposal.Operation != OperationSetOrganizationFeature || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return featureState{}, featureChange{}, ErrProposalInvalidated
	}
	var change featureChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil || change.Feature != string(writableFeature) {
		return featureState{}, featureChange{}, ErrProposalInvalidated
	}
	state, err := f.readState(ctx, tx, proposal.Target.OrganizationID)
	if err != nil {
		return featureState{}, featureChange{}, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return featureState{}, featureChange{}, fmt.Errorf("encode expected feature state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return featureState{}, featureChange{}, ErrStaleState
	}
	return state, change, nil
}

func (f *featureWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, _, err := f.expectedState(ctx, tx, proposal)
	return err
}

func (f *featureWriter) output(proposal Proposal, replay bool) FeatureProposalOutput {
	out := FeatureProposalOutput{ProposalID: proposal.ID.String(), Status: string(proposal.Status), Operation: string(proposal.Operation), ExpiresAt: proposal.ExpiresAt.UTC().Format(time.RFC3339), Replay: replay, ResultCode: proposal.ResultCode}
	if proposal.Status == ProposalPendingApproval {
		out.Preview = proposal.Preview
		out.ApprovalURL = f.baseURL + "/proposals/" + proposal.ID.String()
	}
	if proposal.Status == ProposalSucceeded && len(proposal.ResultPayload) > 0 {
		out.Result = proposal.ResultPayload
	}
	return out
}

func (f *featureWriter) prepare(ctx context.Context, input PrepareFeatureInput) (FeatureProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, f.writes, OperationSetOrganizationFeature)
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	if input.Feature != string(writableFeature) {
		return FeatureProposalOutput{}, errors.New("only the logs feature is available for this write")
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	if input.OrganizationID == "" || len(input.OrganizationID) > 128 || input.OrganizationID != strings.TrimSpace(input.OrganizationID) {
		return FeatureProposalOutput{}, errors.New("provide an exact organization ID")
	}
	if input.RetryKey == "" || len(input.RetryKey) > maxIdempotencyKeyLength {
		return FeatureProposalOutput{}, errors.New("provide a retry key of at most 128 characters")
	}
	tx, err := f.store.db.Begin(ctx)
	if err != nil {
		return FeatureProposalOutput{}, fmt.Errorf("begin feature proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	state, err := f.readState(ctx, tx, input.OrganizationID)
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FeatureProposalOutput{}, fmt.Errorf("finish feature proposal preparation: %w", err)
	}
	args, err := json.Marshal(featureChange{Feature: input.Feature, Enabled: input.Enabled})
	if err != nil {
		return FeatureProposalOutput{}, fmt.Errorf("encode feature change: %w", err)
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return FeatureProposalOutput{}, fmt.Errorf("encode expected feature state: %w", err)
	}
	preview, err := json.Marshal(featurePreview{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Feature: input.Feature, Before: state.Enabled, After: input.Enabled, SideEffects: "Updates the organisation's logs entitlement and its feature cache; records a tenant audit event when state changes."})
	if err != nil {
		return FeatureProposalOutput{}, fmt.Errorf("encode feature preview: %w", err)
	}
	proposal, replay, err := f.store.Create(ctx, owner, NewProposal{Operation: OperationSetOrganizationFeature, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	return f.output(proposal, replay), nil
}

func (f *featureWriter) execute(ctx context.Context, input ProposalIDInput) (FeatureProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, f.writes, OperationSetOrganizationFeature)
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	id, err := uuid.Parse(input.ProposalID)
	if err != nil {
		return FeatureProposalOutput{}, ErrProposalNotFound
	}
	proposal, err := f.store.GetForOwner(ctx, id, owner)
	if err != nil || proposal.Operation != OperationSetOrganizationFeature {
		return FeatureProposalOutput{}, ErrProposalNotFound
	}
	var desired bool
	execution := ProposalExecution{
		Lock: func(ctx context.Context, proposal Proposal) (*pgxpool.Conn, func(), error) {
			return f.mutator.LockFeatureChange(ctx, proposal.Target.OrganizationID, writableFeature)
		},
		Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
			_, change, err := f.expectedState(ctx, tx, proposal)
			if err != nil {
				return "", nil, err
			}
			desired = change.Enabled
			ctx = contextvalues.SetActingSurface(ctx, "admin_mcp")
			ctx = contextvalues.SetOAuthClientID(ctx, authority.Principal.ClientID)
			name := authority.Staff.Name
			if name == "" {
				name = authority.Staff.Email
			}
			actor := productfeatures.MutationActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, authority.Staff.OIDCSubject), DisplayName: &name}
			changed, err := f.mutator.ApplyFeatureChangeTx(ctx, tx, proposal.Target.OrganizationID, writableFeature, change.Enabled, actor)
			if err != nil {
				return "", nil, fmt.Errorf("apply feature change: %w", err)
			}
			result, err := json.Marshal(struct {
				Changed bool `json:"changed"`
			}{Changed: changed})
			if err != nil {
				return "", nil, fmt.Errorf("encode feature receipt: %w", err)
			}
			return "succeeded", result, nil
		},
		AfterCommit: func(ctx context.Context, receipt Proposal) {
			f.mutator.StoreCommittedFeatureChange(ctx, receipt.Target.OrganizationID, writableFeature, desired)
		},
	}
	receipt, replay, err := f.store.Execute(ctx, owner, id, time.Now(), execution)
	if err != nil {
		return FeatureProposalOutput{}, err
	}
	return f.output(receipt, replay), nil
}

func registerFeatureWriteTools(server *mcp.Server, feature *featureWriter) {
	if feature == nil || !feature.writes.OperationEnabled(OperationSetOrganizationFeature) {
		return
	}
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_set_organization_feature", Title: "Prepare Organization Logs Feature Change", Description: "Prepare an exact, single-organization logs entitlement change. Returns a server-stored before/after preview and a private staff approval URL. Does not make the change. Requires admin:write; only logs is currently supported."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareFeatureInput) (*mcp.CallToolResult, FeatureProposalOutput, error) {
		out, err := feature.prepare(ctx, input)
		return nil, out, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "execute_admin_proposal", Title: "Execute Approved Staff Proposal", Description: "Execute the exact stored and separately browser-approved logs change once. Takes only its proposal ID, no target or replacement arguments. A committed receipt replays without writing again."}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProposalIDInput) (*mcp.CallToolResult, FeatureProposalOutput, error) {
		out, err := feature.execute(ctx, input)
		return nil, out, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "get_admin_proposal_status", Title: "Get Staff Proposal Status", Description: "Read the bounded status or committed receipt for an exact proposal owned by this staff subject and OAuth client. Does not change or approve a proposal.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProposalIDInput) (*mcp.CallToolResult, FeatureProposalOutput, error) {
		authority, err := requireWriteAuthority(ctx, feature.writes, OperationSetOrganizationFeature)
		if err != nil {
			return nil, FeatureProposalOutput{}, err
		}
		owner, err := ownerFromAuthority(authority)
		if err != nil {
			return nil, FeatureProposalOutput{}, err
		}
		id, err := uuid.Parse(input.ProposalID)
		if err != nil {
			return nil, FeatureProposalOutput{}, ErrProposalNotFound
		}
		proposal, err := feature.store.GetForOwner(ctx, id, owner)
		if err != nil || proposal.Operation != OperationSetOrganizationFeature {
			return nil, FeatureProposalOutput{}, ErrProposalNotFound
		}
		return nil, feature.output(proposal, true), nil
	})
}

// AttachFeatureWrites enables only the reviewed feature operation. All other
// operation switches remain unavailable even if a server is misconfigured.
func AttachFeatureWrites(runtime *Runtime, oauth *StaffOAuth, features *productfeatures.Client, writes WriteConfig) error {
	if runtime == nil || oauth == nil || oauth.Approval == nil || features == nil {
		return errors.New("staff feature writes are not configured")
	}
	for _, op := range writes.EnabledOperations() {
		if op != OperationSetOrganizationFeature {
			return fmt.Errorf("admin MCP write operation %q is not implemented", op)
		}
	}
	feature := &featureWriter{store: oauth.Approval.store, mutator: productfeatures.NewMutator(features, audit.NewLogger()), writes: writes, baseURL: oauth.Resource()}
	oauth.Approval.revalidate = map[WriteOperation]ProposalRevalidator{OperationSetOrganizationFeature: feature.revalidate} //nolint:exhaustive // Other operations have no approval handler until implemented.
	registerFeatureWriteTools(runtime.server, feature)
	return nil
}
