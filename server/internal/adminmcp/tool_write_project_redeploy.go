//nolint:exhaustruct // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/deployments"
)

const projectRedeploySideEffects = "Creates a new deployment from the active one and processes every source again, deploying function runners again. External MCP sources are not carried over. The customer's deployment history and audit log show the redeploy, and their webhooks receive it. The current deployment stays active until the new one succeeds, and stays active if it fails."

// RedeployProjectInput is one exact project redeploy.
type RedeployProjectInput struct {
	// OrganizationID selects an exact canonical organization, never a slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`

	// ProjectID selects an exact project, never a slug.
	ProjectID string `json:"project_id" jsonschema:"Exact project ID from list_organization_projects, not a slug"`

	// ExpectedActiveDeploymentID is the deployment the user confirmed replacing.
	ExpectedActiveDeploymentID string `json:"expected_active_deployment_id" jsonschema:"Active deployment ID from get_project_redeploy_preview, shown to the user before they confirmed"`
}

// projectRedeployReceipt records the clone. Processing runs afterwards and can
// fail, so it says nothing about the outcome; get_project_summary shows whether
// the new deployment became active.
type projectRedeployReceipt struct {
	OrganizationID     string `json:"organization_id"`
	ProjectID          string `json:"project_id"`
	ProjectName        string `json:"project_name"`
	SourceDeploymentID string `json:"source_deployment_id"`
	DeploymentID       string `json:"deployment_id"`
	ExternalMCPsBefore int    `json:"external_mcps_before"`
	ExternalMCPsAfter  int    `json:"external_mcps_after"`
	SideEffects        string `json:"side_effects"`
}

// projectRedeployWriter redeploys one exact project. The confirmed active
// deployment and the in-flight check make a repeated call safe: the first call
// is still processing, so the repeat is refused rather than cloning again.
type projectRedeployWriter struct {
	runner     *writeRunner
	redeployer admin.ProjectRedeployer
	writes     WriteConfig
	now        func() time.Time
}

func (w *projectRedeployWriter) apply(ctx context.Context, input RedeployProjectInput) (projectRedeployReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationRedeployProject)
	if err != nil {
		return projectRedeployReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return projectRedeployReceipt{}, err
	}
	projectID, err := parseExactProjectID(input.ProjectID)
	if err != nil {
		return projectRedeployReceipt{}, err
	}
	expectedActive, err := uuid.Parse(input.ExpectedActiveDeploymentID)
	if err != nil {
		return projectRedeployReceipt{}, errors.New("provide the exact active deployment ID from get_project_redeploy_preview")
	}

	target := writeTarget{OrganizationID: input.OrganizationID, ResourceKind: "project", ResourceID: projectID.String()}
	receipt, err := runWrite(ctx, w.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (projectRedeployReceipt, error) {
		state, err := admin.LockProjectRedeployStateTx(ctx, tx, w.runner.logger, input.OrganizationID, projectID, w.now())
		switch {
		case errors.Is(err, admin.ErrProjectRedeployTargetNotFound):
			return projectRedeployReceipt{}, errors.New("no live project with that ID in that organization")
		case errors.Is(err, admin.ErrProjectHasNoActiveDeployment):
			return projectRedeployReceipt{}, errors.New("project has no active deployment; nothing would change")
		case err != nil:
			return projectRedeployReceipt{}, fmt.Errorf("read project redeploy state: %w", err)
		}
		if state.ActiveDeploymentID != expectedActive {
			return projectRedeployReceipt{}, ErrStaleState
		}
		if state.InFlightDeploymentID.Valid {
			return projectRedeployReceipt{}, fmt.Errorf("%w: wait for deployment %s; get_project_summary shows the active deployment", admin.ErrProjectRedeployInProgress, state.InFlightDeploymentID.UUID)
		}
		ctx, actor, displayName := staffMutation(ctx, authority)
		clone, err := w.redeployer.Prepare(ctx, tx, deployments.StaffRedeploy{
			OrganizationID:     state.OrganizationID,
			ProjectID:          state.ProjectID,
			SourceDeploymentID: state.ActiveDeploymentID,
			Actor:              actor,
			ActorDisplayName:   displayName,
		})
		if err != nil {
			return projectRedeployReceipt{}, fmt.Errorf("prepare project redeploy: %w", err)
		}
		return projectRedeployReceipt{
			OrganizationID: state.OrganizationID, ProjectID: state.ProjectID.String(), ProjectName: state.ProjectName,
			SourceDeploymentID: state.ActiveDeploymentID.String(), DeploymentID: clone.ID,
			ExternalMCPsBefore: state.ExternalMCPs, ExternalMCPsAfter: 0, SideEffects: projectRedeploySideEffects,
		}, nil
	})
	if err != nil {
		return projectRedeployReceipt{}, err
	}

	deploymentID, err := uuid.Parse(receipt.DeploymentID)
	if err != nil {
		return projectRedeployReceipt{}, fmt.Errorf("parse redeployed deployment id: %w", err)
	}
	if err := w.redeployer.Dispatch(context.WithoutCancel(ctx), projectID, deploymentID, expectedActive); err != nil {
		return projectRedeployReceipt{}, fmt.Errorf("deployment %s was created but did not start, and the active deployment is unchanged: %w", receipt.DeploymentID, err)
	}
	return receipt, nil
}

func (w *projectRedeployWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationRedeployProject), Title: "Redeploy Project", Description: "Redeploy one exact project's active deployment. " + projectRedeploySideEffects + " Refused while a newer deployment is still processing. Read get_project_redeploy_preview first. Processing continues after this returns; get_project_summary shows whether the new deployment became active."}, w.apply)
}
