//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// writableFeatures is the reviewed allowlist for this write, mapped to the
// side effects shown on the approval page. Remote-session auto-refresh uses
// the policy setter only while enforcement is off, so its companion flag is
// never changed. Additional features require their own side-effect review
// before joining this list.
var writableFeatures = map[productfeatures.Feature]string{ //nolint:exhaustive // Only reviewed features are writable.
	productfeatures.FeatureSSO:                      "Enables or disables creation of WorkOS SSO setup portal links. Does not create or remove SSO connections, change existing SSO login behaviour, or change the WorkOS-managed SSO connection status. Does not return portal links or change account type, billing or trials. Updates the feature cache and records a tenant audit event when state changes.",
	productfeatures.FeatureSCIM:                     "Enables or disables creation of WorkOS directory-sync setup portal links. Does not create or remove directories, stop existing directory sync, or change directory-managed membership, roles or the WorkOS-managed SCIM connection status. Does not return portal links or change account type, billing or trials. Updates the feature cache and records a tenant audit event when state changes.",
	productfeatures.FeatureLogs:                     "Updates the organisation's logs entitlement and its feature cache; records a tenant audit event when state changes.",
	productfeatures.FeatureConsentToolFiltering:     "Shows or hides the tool picker on this organisation's MCP consent screens and updates its feature cache; tool selections already stored are still enforced. Records a tenant audit event when state changes.",
	productfeatures.FeatureRemoteSessionAutoRefresh: "Shows or hides the Auto refresh opt-in on consent screens and allows or stops automatic refresh of eligible sessions whose stored preference is on. Stored per-session choices are unchanged. Requires enforced refresh to remain off; enforcement is not changed. Updates feature caches and records a tenant audit event when state changes.",
}

func writableFeature(name string) (productfeatures.Feature, bool) {
	feature := productfeatures.Feature(name)
	_, ok := writableFeatures[feature]
	return feature, ok
}

type PrepareFeatureInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	Feature        string `json:"feature" jsonschema:"Feature to change: logs, consent_tool_filtering, remote_session_auto_refresh, sso, or scim; sso/scim change setup portal entitlements only, not existing connections; remote_session_auto_refresh requires enforced refresh to be off"`
	Enabled        bool   `json:"enabled" jsonschema:"Desired enabled state"`
	RetryKey       string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
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

	// AutoRefreshEnforced binds the companion policy for auto-refresh proposals
	// without changing the expected-state encoding of other features.
	AutoRefreshEnforced *bool `json:"auto_refresh_enforced,omitempty"`
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

func (f *featureWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string, feature productfeatures.Feature) (featureState, error) {
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
	enabled, err := featurerepo.New(tx).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(feature)})
	if err != nil {
		return featureState{}, fmt.Errorf("read feature state: %w", err)
	}
	state := featureState{OrganizationID: id, Name: org.Name, Slug: org.Slug, Enabled: enabled, AutoRefreshEnforced: nil}
	if feature == productfeatures.FeatureRemoteSessionAutoRefresh {
		enforced, err := featurerepo.New(tx).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
		if err != nil {
			return featureState{}, fmt.Errorf("read remote session refresh enforcement: %w", err)
		}
		state.AutoRefreshEnforced = new(enforced)
	}
	return state, nil
}

func (f *featureWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (featureState, featureChange, error) {
	if proposal.Operation != OperationSetOrganizationFeature || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return featureState{}, featureChange{}, ErrProposalInvalidated
	}
	var change featureChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil {
		return featureState{}, featureChange{}, ErrProposalInvalidated
	}
	feature, ok := writableFeature(change.Feature)
	if !ok {
		return featureState{}, featureChange{}, ErrProposalInvalidated
	}
	state, err := f.readState(ctx, tx, proposal.Target.OrganizationID, feature)
	if err != nil {
		return featureState{}, featureChange{}, err
	}
	if state.AutoRefreshEnforced != nil && *state.AutoRefreshEnforced {
		return featureState{}, featureChange{}, ErrStaleState
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

func (f *featureWriter) prepare(ctx context.Context, input PrepareFeatureInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, f.writes, OperationSetOrganizationFeature)
	if err != nil {
		return ProposalOutput{}, err
	}
	feature, ok := writableFeature(input.Feature)
	if !ok {
		return ProposalOutput{}, errors.New("only logs, consent_tool_filtering, remote_session_auto_refresh, sso, and scim are available for this write")
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := checkPrepareTarget(input.OrganizationID, input.RetryKey); err != nil {
		return ProposalOutput{}, err
	}
	tx, err := f.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin feature proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	state, err := f.readState(ctx, tx, input.OrganizationID, feature)
	if err != nil {
		return ProposalOutput{}, err
	}
	if state.AutoRefreshEnforced != nil && *state.AutoRefreshEnforced {
		return ProposalOutput{}, errors.New("remote session auto-refresh is enforced; this tool cannot change enforcement")
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish feature proposal preparation: %w", err)
	}
	args, err := json.Marshal(featureChange{Feature: input.Feature, Enabled: input.Enabled})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode feature change: %w", err)
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected feature state: %w", err)
	}
	preview, err := json.Marshal(featurePreview{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Feature: input.Feature, Before: state.Enabled, After: input.Enabled, SideEffects: writableFeatures[feature]})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode feature preview: %w", err)
	}
	proposal, replay, err := f.store.Create(ctx, owner, NewProposal{Operation: OperationSetOrganizationFeature, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, f.baseURL), nil
}

// view renders the stored preview for the approval page. A preview that does
// not name the proposal's own organization is never shown.
func (f *featureWriter) view(proposal Proposal) (proposalView, error) {
	var preview featurePreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.OrganizationID == "" || preview.OrganizationID != proposal.Target.OrganizationID || preview.Feature == "" {
		return proposalView{}, ErrProposalInvalidated
	}
	return proposalView{
		Summary:          fmt.Sprintf("Turn the %s feature %s", preview.Feature, strings.ToLower(enabledLabel(preview.After))),
		OrganizationID:   preview.OrganizationID,
		OrganizationName: preview.Name,
		OrganizationSlug: preview.Slug,
		Changes:          []proposalViewChange{{Setting: preview.Feature + " feature", Before: enabledLabel(preview.Before), After: enabledLabel(preview.After)}},
		SideEffects:      preview.SideEffects,
	}, nil
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "On"
	}
	return "Off"
}

// execution applies the stored feature change under the feature cache locks
// and refreshes the cache once the change has committed. The lock is taken
// for the stored feature before the transaction starts; the store's digest
// check guarantees Run sees the same arguments, and Run re-checks anyway.
func (f *featureWriter) execution(authority writeAuthority) ProposalExecution {
	var locked productfeatures.Feature
	var desired bool
	return ProposalExecution{
		Lock: func(ctx context.Context, proposal Proposal) (*pgxpool.Conn, func(), error) {
			var change featureChange
			if err := json.Unmarshal(proposal.Arguments, &change); err != nil {
				return nil, nil, ErrProposalInvalidated
			}
			feature, ok := writableFeature(change.Feature)
			if !ok {
				return nil, nil, ErrProposalInvalidated
			}
			locked = feature
			return f.mutator.LockFeatureChange(ctx, proposal.Target.OrganizationID, feature)
		},
		Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
			_, change, err := f.expectedState(ctx, tx, proposal)
			if err != nil {
				return "", nil, err
			}
			if locked == "" || productfeatures.Feature(change.Feature) != locked {
				return "", nil, ErrProposalInvalidated
			}
			desired = change.Enabled
			ctx, principal, name := staffMutation(ctx, authority)
			actor := productfeatures.MutationActor{Principal: principal, DisplayName: name}
			changed, err := f.mutator.ApplyFeatureChangeTx(ctx, tx, proposal.Target.OrganizationID, locked, change.Enabled, actor)
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
			f.mutator.StoreCommittedFeatureChange(ctx, receipt.Target.OrganizationID, locked, desired)
		},
	}
}

func (f *featureWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_set_organization_feature", Title: "Prepare Organization Feature Change", Description: "Prepare an exact, single-organization change to logs, consent_tool_filtering, remote_session_auto_refresh, sso, or scim. SSO and SCIM changes affect setup portal entitlements only, not existing identity-provider connections, login, directory sync, membership or roles; no portal links are returned. Remote-session auto-refresh changes require enforcement to be off and leave stored per-session choices unchanged; they cannot change enforcement. Returns a server-stored before/after preview and a private staff approval URL. Does not make the change. Requires admin:write; no other features are supported."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareFeatureInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := f.prepare(ctx, input)
		return nil, out, err
	})
}
