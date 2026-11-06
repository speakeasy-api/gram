//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// writableFeatures is the reviewed allowlist for this write, mapped to the
// side effects returned with each change. Remote-session auto-refresh uses
// the policy setter only while enforcement is off, so its companion flag is
// never changed. Additional features require their own side-effect review
// before joining this list.
var writableFeatures = map[productfeatures.Feature]string{ //nolint:exhaustive // Only reviewed features are writable.
	productfeatures.FeatureSSO:                      "Enables or disables creation of WorkOS SSO setup portal links. Does not create or remove SSO connections, change existing SSO login behaviour, or change the WorkOS-managed SSO connection status. Does not return portal links or change account type, billing or trials. Updates the feature cache and records a tenant audit event when state changes.",
	productfeatures.FeatureSCIM:                     "Enables or disables creation of WorkOS directory-sync setup portal links. Does not create or remove directories, stop existing directory sync, or change directory-managed membership, roles or the WorkOS-managed SCIM connection status. Does not return portal links or change account type, billing or trials. Updates the feature cache and records a tenant audit event when state changes.",
	productfeatures.FeatureLogs:                     "Updates the organisation's logs entitlement and its feature cache; records a tenant audit event when state changes.",
	productfeatures.FeatureRemoteSessionAutoRefresh: "Shows or hides the Auto refresh opt-in on consent screens and allows or stops automatic refresh of eligible sessions whose stored preference is on. Stored per-session choices are unchanged. Requires enforced refresh to remain off; enforcement is not changed. Updates feature caches and records a tenant audit event when state changes.",
}

func writableFeature(name string) (productfeatures.Feature, bool) {
	feature := productfeatures.Feature(name)
	_, ok := writableFeatures[feature]
	return feature, ok
}

// FeatureInput is one exact feature change.
type FeatureInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	Feature        string `json:"feature" jsonschema:"Feature to change: logs, remote_session_auto_refresh, sso, or scim; sso/scim change setup portal entitlements only, not existing connections; remote_session_auto_refresh requires enforced refresh to be off"`
	Enabled        bool   `json:"enabled" jsonschema:"Desired enabled state"`
}

type featureState struct {
	OrganizationID string
	Name           string
	Slug           string
	Enabled        bool
	// AutoRefreshEnforced is the companion policy, read only for auto-refresh.
	AutoRefreshEnforced *bool
}

type featureReceipt struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Feature        string `json:"feature"`
	Before         bool   `json:"before"`
	After          bool   `json:"after"`
	Changed        bool   `json:"changed"`
	SideEffects    string `json:"side_effects"`
}

type featureWriter struct {
	runner  *writeRunner
	mutator *productfeatures.Mutator
	writes  WriteConfig
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

// apply makes the change under the feature cache locks, which also cover the
// cache refresh after commit.
func (f *featureWriter) apply(ctx context.Context, input FeatureInput) (featureReceipt, error) {
	authority, err := requireWriteAuthority(ctx, f.writes, OperationSetOrganizationFeature)
	if err != nil {
		return featureReceipt{}, err
	}
	feature, ok := writableFeature(input.Feature)
	if !ok {
		return featureReceipt{}, errors.New("only logs, remote_session_auto_refresh, sso, and scim are available for this write")
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return featureReceipt{}, err
	}
	conn, release, err := f.mutator.LockFeatureChange(ctx, input.OrganizationID, feature)
	if err != nil {
		return featureReceipt{}, fmt.Errorf("lock feature change: %w", err)
	}
	defer release()
	receipt, err := runWriteOn(ctx, f.runner, conn, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (featureReceipt, error) {
		state, err := f.readState(ctx, tx, input.OrganizationID, feature)
		if err != nil {
			return featureReceipt{}, err
		}
		if state.AutoRefreshEnforced != nil && *state.AutoRefreshEnforced {
			return featureReceipt{}, errors.New("remote session auto-refresh is enforced; this tool cannot change enforcement")
		}
		ctx, principal, name := staffMutation(ctx, authority)
		actor := productfeatures.MutationActor{Principal: principal, DisplayName: name}
		changed, err := f.mutator.ApplyFeatureChangeTx(ctx, tx, input.OrganizationID, feature, input.Enabled, actor)
		if err != nil {
			return featureReceipt{}, fmt.Errorf("apply feature change: %w", err)
		}
		return featureReceipt{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Feature: input.Feature, Before: state.Enabled, After: input.Enabled, Changed: changed, SideEffects: writableFeatures[feature]}, nil
	})
	if err != nil {
		return featureReceipt{}, err
	}
	f.mutator.StoreCommittedFeatureChange(context.WithoutCancel(ctx), input.OrganizationID, feature, input.Enabled)
	return receipt, nil
}

func (f *featureWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationSetOrganizationFeature), Title: "Set Organization Feature", Description: "Make an exact, single-organization change to logs, remote_session_auto_refresh, sso, or scim. SSO and SCIM changes affect setup portal entitlements only, not existing identity-provider connections, login, directory sync, membership or roles; no portal links are returned. Remote-session auto-refresh changes require enforcement to be off and leave stored per-session choices unchanged; they cannot change enforcement. No other features are supported."}, f.apply)
}
