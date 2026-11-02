package risk

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/approvalserver"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// PluginPublishFunc enqueues a republish of a project's plugins after the
// transaction that changed them commits. pluginCreated is true when that
// transaction created the Default plugin.
type PluginPublishFunc func(ctx context.Context, projectID uuid.UUID, userID string, pluginCreated bool)

// WithApprovalsServerPublishing lets the first approval policy republish the
// Default plugin it adds the approvals server to. Without it the server is
// still added, and the next publish picks it up.
func (s *Service) WithApprovalsServerPublishing(requests plugins.PublicationRequests, publish PluginPublishFunc) *Service {
	s.publicationRequests = requests
	s.pluginPublish = publish
	return s
}

// createPolicy saves a new policy and, when it is an enabled approval policy,
// sets up the project's approvals server in the same transaction.
func (s *Service) createPolicy(ctx context.Context, authCtx *contextvalues.AuthContext, mutation policycore.CreateMutation) (policycore.MutationResult, *approvalserver.Outcome, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return policycore.MutationResult{}, nil, oops.E(oops.CodeUnexpected, err, "begin risk policy create").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	result, err := s.policies.CreatePolicyInTransaction(ctx, tx, mutation)
	if err != nil {
		return policycore.MutationResult{}, nil, s.policyMutationError(ctx, err)
	}
	setup, err := s.ensureApprovalsServer(ctx, tx, authCtx, result.Row)
	if err != nil {
		return policycore.MutationResult{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return policycore.MutationResult{}, nil, oops.E(oops.CodeUnexpected, err, "commit risk policy create").LogError(ctx, s.logger)
	}
	s.policies.AfterCreatePolicy(ctx, result)
	s.afterApprovalsServer(ctx, authCtx, setup)
	return result, setup, nil
}

// updatePolicy saves a policy change and, when it leaves an enabled approval
// policy, sets up the project's approvals server in the same transaction.
func (s *Service) updatePolicy(ctx context.Context, authCtx *contextvalues.AuthContext, mutation policycore.UpdateMutation) (policycore.MutationResult, *approvalserver.Outcome, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return policycore.MutationResult{}, nil, oops.E(oops.CodeUnexpected, err, "begin risk policy update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	result, err := s.policies.UpdatePolicyInTransaction(ctx, tx, mutation)
	if err != nil {
		return policycore.MutationResult{}, nil, s.policyMutationError(ctx, err)
	}
	setup, err := s.ensureApprovalsServer(ctx, tx, authCtx, result.Row)
	if err != nil {
		return policycore.MutationResult{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return policycore.MutationResult{}, nil, oops.E(oops.CodeUnexpected, err, "commit risk policy update").LogError(ctx, s.logger)
	}
	s.policies.AfterUpdatePolicy(ctx, result)
	s.afterApprovalsServer(ctx, authCtx, setup)
	return result, setup, nil
}

// ensureApprovalsServer gives the project its approvals server the first time
// an enabled approval policy is saved. It returns nil when the policy is not
// one, or when the server already existed.
func (s *Service) ensureApprovalsServer(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, row repo.RiskPolicy) (*approvalserver.Outcome, error) {
	if row.Action != policycore.ActionRequireApproval || !row.Enabled {
		return nil, nil
	}
	outcome, err := approvalserver.Ensure(ctx, tx, s.audit, authCtx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set up the approvals server").LogError(ctx, s.logger)
	}
	if !outcome.Created {
		return nil, nil
	}
	if outcome.AddedToDefaultPlugin {
		if err := s.publicationRequests.Project(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "request plugin publication").LogError(ctx, s.logger)
		}
	}
	return &outcome, nil
}

// afterApprovalsServer republishes the Default plugin the approvals server
// just joined. It runs after commit, so a rolled-back create publishes
// nothing.
func (s *Service) afterApprovalsServer(ctx context.Context, authCtx *contextvalues.AuthContext, setup *approvalserver.Outcome) {
	if setup == nil || !setup.AddedToDefaultPlugin || s.pluginPublish == nil {
		return
	}
	s.pluginPublish(ctx, *authCtx.ProjectID, authCtx.UserID, setup.DefaultPluginCreated)
}

// policyWithSetup is the policy view, with the approvals server setup when
// this change created it.
func (s *Service) policyWithSetup(ctx context.Context, row repo.RiskPolicy, setup *approvalserver.Outcome) (*types.RiskPolicy, error) {
	view, err := s.policyToType(ctx, row)
	if err != nil {
		return nil, err
	}
	if setup == nil {
		return view, nil
	}
	view.ApprovalsServerSetup = &types.RiskPolicyApprovalsServerSetup{
		McpServerID:          setup.Server.ID.String(),
		McpServerSlug:        setup.Server.Slug,
		PluginServerName:     approval.MCPServerName,
		AddedToDefaultPlugin: setup.AddedToDefaultPlugin,
	}
	return view, nil
}
