package platformmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gendeployments "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deploymentrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
)

const (
	redeployDeploymentToolName                  = "redeploy_deployment"
	noActiveDeployment                          = "none"
	DeploymentRedeployConnectionLimitName       = "platform_mcp_deployment_redeploy_connection"
	DeploymentRedeployOrganizationLimitName     = "platform_mcp_deployment_redeploy_organization"
	DeploymentRedeploysPerConnectionPerMinute   = 2
	DeploymentRedeploysPerOrganizationPerMinute = 10
)

// DeploymentRedeployManagement shares authorization, cloning, audit, and
// processing with the dashboard. Only the transaction boundary differs: the
// clone and its Platform MCP receipt must commit together before processing.
type DeploymentRedeployManagement interface {
	GetDeployment(context.Context, *gendeployments.GetDeploymentPayload) (*gendeployments.GetDeploymentResult, error)
	PrepareRedeploy(context.Context, pgx.Tx, *gendeployments.RedeployPayload) (*types.Deployment, error)
	DispatchRedeploy(context.Context, string, uuid.UUID) (*gendeployments.RedeployResult, error)
}

type RedeployDeploymentInput struct {
	ProjectID                  string `json:"project_id" jsonschema:"exact project UUID; never inferred"`
	DeploymentID               string `json:"deployment_id,omitempty" jsonschema:"exact source deployment UUID returned by the preview; omit only for a preview of the active deployment"`
	ExpectedActiveDeploymentID string `json:"expected_active_deployment_id,omitempty" jsonschema:"active_deployment_id from the preview, or none if the project has no active deployment; required for confirmation"`
	IdempotencyKey             string `json:"idempotency_key" jsonschema:"stable key of 1 to 128 characters for this redeployment; reuse unchanged on retries"`
	Confirmed                  bool   `json:"confirmed" jsonschema:"true only after the user confirms the exact project, source snapshot, active snapshot, and project-wide impact shown in the preview"`
}

type DeploymentRedeployPreview struct {
	ProjectID          string `json:"project_id"`
	SourceDeploymentID string `json:"source_deployment_id"`
	ActiveDeploymentID string `json:"active_deployment_id"`
	SourceStatus       string `json:"source_status"`
	OpenAPIAssets      int    `json:"openapi_assets"`
	FunctionAssets     int    `json:"function_assets"`
	Packages           int    `json:"packages"`
	ExternalMCPs       int    `json:"external_mcps"`
	Scope              string `json:"scope"`
}

type RedeployDeploymentOutput struct {
	ProjectID          string `json:"project_id"`
	SourceDeploymentID string `json:"source_deployment_id"`
	DeploymentID       string `json:"deployment_id"`
	Status             string `json:"status"`
	Verified           bool   `json:"verified" jsonschema:"true when the committed deployment was read and matched the expected project and source; not a readiness or success guarantee"`
	Active             *bool  `json:"active,omitempty" jsonschema:"whether this deployment was observed as active; omitted when active state could not be read"`
	Processing         string `json:"processing"`
	Replayed           bool   `json:"replayed"`
	DashboardPath      string `json:"dashboard_path"`
}

type deploymentRedeployError struct {
	Code    string
	Message string
	Preview *DeploymentRedeployPreview
}

func (e *deploymentRedeployError) Error() string { return e.Message }
func redeployInvalid(message string) error {
	return &deploymentRedeployError{Code: "invalid_request", Message: message, Preview: nil}
}
func redeployConflict(message string) error {
	return &deploymentRedeployError{Code: "conflict", Message: message, Preview: nil}
}
func redeployUnavailable(_ error) error {
	return &deploymentRedeployError{Code: unavailableCode, Message: "Deployment redeployment is temporarily unavailable. If a deployment ID was returned, keep the same idempotency key and inspect that deployment before retrying.", Preview: nil}
}

type deploymentRedeployReceipt struct {
	ProjectID          string `json:"project_id"`
	SourceDeploymentID string `json:"source_deployment_id"`
	DeploymentID       string `json:"deployment_id"`
}
type deploymentRedeployTransaction func(context.Context, pgx.Tx) (deploymentRedeployReceipt, error)
type deploymentRedeployReceipts interface {
	Execute(context.Context, Principal, ResolvedProject, RedeployDeploymentInput, func(context.Context) error, deploymentRedeployTransaction) (OperationReceipt, error)
}
type deploymentRedeployReceiptStore struct{ db *pgxpool.Pool }

func (s *deploymentRedeployReceiptStore) Execute(ctx context.Context, principal Principal, project ResolvedProject, input RedeployDeploymentInput, charge func(context.Context) error, mutate deploymentRedeployTransaction) (OperationReceipt, error) {
	normalized := struct {
		Source string `json:"source"`
		Active string `json:"active"`
	}{Source: input.DeploymentID, Active: input.ExpectedActiveDeploymentID}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return OperationReceipt{}, redeployUnavailable(err)
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-redeploy-v1\x00"), payload...))
	return executeChargedMutationReceipt(ctx, charge, mutationReceiptExecution[deploymentRedeployReceipt]{
		DB: s.db, Now: time.Now, Principal: principal, Project: project, Operation: redeployDeploymentToolName,
		IdempotencyKey: input.IdempotencyKey, InputHash: hex.EncodeToString(digest[:]), Label: "deployment redeploy",
		Invalid:  func(error) error { return redeployInvalid("The redeployment caller identity is invalid.") },
		Conflict: redeployConflict, Unavailable: redeployUnavailable,
		ValidateReplay: func(payload []byte) bool { _, err := decodeDeploymentRedeployReceipt(payload); return err == nil },
		EncodeResult: func(result deploymentRedeployReceipt) ([]byte, error) {
			encoded, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode redeployment receipt: %w", err)
			}
			if _, err := decodeDeploymentRedeployReceipt(encoded); err != nil {
				return nil, err
			}
			return encoded, nil
		},
		Mutate: mutate,
	})
}

func decodeDeploymentRedeployReceipt(payload []byte) (deploymentRedeployReceipt, error) {
	var result deploymentRedeployReceipt
	if len(payload) == 0 || len(payload) > 4096 {
		return result, ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, ErrUnavailable
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return result, ErrUnavailable
	}
	for _, value := range []string{result.ProjectID, result.SourceDeploymentID, result.DeploymentID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil {
			return result, ErrUnavailable
		}
	}
	if result.SourceDeploymentID == result.DeploymentID {
		return result, ErrUnavailable
	}
	return result, nil
}

type DeploymentRedeployService struct {
	management       DeploymentRedeployManagement
	resolveProject   func(context.Context, Principal, FindMCPInput) (ResolvedProject, error)
	organizations    OrganizationSlugResolver
	activeDeployment func(context.Context, pgx.Tx, uuid.UUID) (string, error)
	receipts         deploymentRedeployReceipts
	budget           OperationBudget
}

func NewDeploymentRedeployService(db *pgxpool.Pool, management DeploymentRedeployManagement, projects *PostgresReader, budget OperationBudget) *DeploymentRedeployService {
	if db == nil || management == nil || projects == nil || !budget.valid() {
		return nil
	}
	return &DeploymentRedeployService{
		management: management,
		resolveProject: func(ctx context.Context, p Principal, input FindMCPInput) (ResolvedProject, error) {
			return projects.resolveProjectWithScope(ctx, p, input, authz.ScopeProjectWrite)
		},
		organizations: NewPostgresOrganizationSlugResolver(db),
		activeDeployment: func(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (string, error) {
			q := deploymentrepo.New(db)
			if tx != nil {
				q = deploymentrepo.New(tx)
			}
			id, err := q.GetActiveDeploymentID(ctx, projectID)
			if errors.Is(err, pgx.ErrNoRows) {
				return noActiveDeployment, nil
			}
			if err != nil {
				return "", fmt.Errorf("read active deployment: %w", err)
			}
			return id.String(), nil
		},
		receipts: &deploymentRedeployReceiptStore{db: db}, budget: budget,
	}
}
func (s *DeploymentRedeployService) valid() bool {
	return s != nil && s.management != nil && s.resolveProject != nil && s.organizations != nil && s.activeDeployment != nil && s.receipts != nil && s.budget.valid()
}

func (s *DeploymentRedeployService) Redeploy(ctx context.Context, principal Principal, input RedeployDeploymentInput) (RedeployDeploymentOutput, error) {
	var zero RedeployDeploymentOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil || projectID == uuid.Nil || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 128 {
		return zero, redeployInvalid("Name an exact project and a stable idempotency key of 1 to 128 characters.")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || principal.UserID == "" || principal.OrganizationID == "" || authCtx.UserID != principal.UserID || authCtx.ActiveOrganizationID != principal.OrganizationID {
		return zero, ErrForbidden
	}
	project, err := s.resolveProject(ctx, principal, FindMCPInput{ProjectID: projectID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return zero, err
	}
	if project.ID != projectID {
		return zero, ErrForbidden
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return zero, ErrUnavailable
	}
	scoped := *authCtx
	scoped.OrganizationSlug = organizationSlug
	scoped.ProjectID = &project.ID
	scoped.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &scoped)
	if input.Confirmed && (input.DeploymentID == "" || input.ExpectedActiveDeploymentID == "") {
		return zero, redeployInvalid("Preview first, then confirm its exact source and active deployment IDs.")
	}
	if input.ExpectedActiveDeploymentID != "" && input.ExpectedActiveDeploymentID != noActiveDeployment {
		id, parseErr := uuid.Parse(input.ExpectedActiveDeploymentID)
		if parseErr != nil || id == uuid.Nil {
			return zero, redeployInvalid("The expected active deployment must be a UUID or none.")
		}
		input.ExpectedActiveDeploymentID = id.String()
	}
	if input.DeploymentID == "" {
		input.DeploymentID, err = s.activeDeployment(ctx, nil, project.ID)
		if err != nil {
			return zero, redeployUnavailable(err)
		}
		if input.DeploymentID == noActiveDeployment {
			return zero, &deploymentRedeployError{Code: "not_found", Message: "This project has no active deployment. Supply an exact source deployment ID to preview.", Preview: nil}
		}
	}
	sourceID, err := uuid.Parse(input.DeploymentID)
	if err != nil || sourceID == uuid.Nil {
		return zero, redeployInvalid("The source deployment must be an exact UUID.")
	}
	input.DeploymentID = sourceID.String()
	source, err := s.management.GetDeployment(ctx, &gendeployments.GetDeploymentPayload{ID: input.DeploymentID, ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("read redeployment source: %w", err)
	}
	if source == nil || source.ID != input.DeploymentID || source.ProjectID != project.ID.String() || source.OrganizationID != principal.OrganizationID {
		return zero, ErrForbidden
	}
	if !input.Confirmed {
		active, err := s.activeDeployment(ctx, nil, project.ID)
		if err != nil {
			return zero, redeployUnavailable(err)
		}
		preview := &DeploymentRedeployPreview{ProjectID: project.ID.String(), SourceDeploymentID: source.ID, ActiveDeploymentID: active, SourceStatus: source.Status, OpenAPIAssets: len(source.Openapiv3Assets), FunctionAssets: len(source.FunctionsAssets), Packages: len(source.Packages), ExternalMCPs: len(source.ExternalMcps), Scope: "whole_project"}
		return zero, &deploymentRedeployError{Code: "confirmation_required", Message: "Nothing was redeployed. This clones and reprocesses the whole project snapshot, not one server: all OpenAPI, function, package and external MCP sources are included. Redeploying an older snapshot can replace newer project tools and configuration. Show the exact project, source, active deployment and source counts, obtain explicit confirmation, then resend these IDs with the same key and confirmed:true.", Preview: preview}
	}
	charge := func(ctx context.Context) error { return s.budget.Allow(ctx, principal) }
	receipt, err := s.receipts.Execute(ctx, principal, project, input, charge, func(ctx context.Context, tx pgx.Tx) (deploymentRedeployReceipt, error) {
		active, err := s.activeDeployment(ctx, tx, project.ID)
		if err != nil {
			return deploymentRedeployReceipt{}, err
		}
		if active != input.ExpectedActiveDeploymentID {
			return deploymentRedeployReceipt{}, redeployConflict("The active deployment changed after the preview. Preview again and reconfirm before using a new idempotency key.")
		}
		deployed, err := s.management.PrepareRedeploy(ctx, tx, &gendeployments.RedeployPayload{DeploymentID: input.DeploymentID, ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return deploymentRedeployReceipt{}, fmt.Errorf("prepare redeployment: %w", err)
		}
		if deployed == nil || deployed.ProjectID != project.ID.String() || deployed.OrganizationID != principal.OrganizationID || deployed.ClonedFrom == nil || *deployed.ClonedFrom != input.DeploymentID {
			return deploymentRedeployReceipt{}, ErrUnavailable
		}
		return deploymentRedeployReceipt{ProjectID: project.ID.String(), SourceDeploymentID: input.DeploymentID, DeploymentID: deployed.ID}, nil
	})
	if err != nil {
		return zero, fmt.Errorf("commit redeployment receipt: %w", err)
	}
	stored, err := decodeDeploymentRedeployReceipt(receipt.ResultPayload)
	if err != nil || stored.ProjectID != project.ID.String() || stored.SourceDeploymentID != input.DeploymentID {
		return zero, redeployUnavailable(err)
	}
	out := RedeployDeploymentOutput{ProjectID: stored.ProjectID, SourceDeploymentID: stored.SourceDeploymentID, DeploymentID: stored.DeploymentID, Status: "unknown", Verified: false, Active: nil, Processing: "verification_unavailable", Replayed: receipt.Replayed, DashboardPath: "/" + url.PathEscape(organizationSlug) + "/projects/" + url.PathEscape(project.Slug) + "/deployments/" + stored.DeploymentID}
	current, err := s.readCommitted(ctx, principal, stored)
	if err != nil {
		return out, nil
	}
	out.Status = current.Status
	out.Verified = true
	out.Processing = "observed"
	if current.Status == "created" {
		if receipt.Replayed {
			if err := charge(ctx); err != nil {
				out.Processing = "rate_limited"
				if !errors.Is(err, ErrOperationRateLimited) {
					out.Processing = "unavailable"
				}
				return out, nil
			}
		}
		expectedActive := uuid.Nil
		if input.ExpectedActiveDeploymentID != noActiveDeployment {
			expectedActive, _ = uuid.Parse(input.ExpectedActiveDeploymentID)
		}
		_, processErr := s.management.DispatchRedeploy(ctx, stored.DeploymentID, expectedActive)
		out.Processing = "requested"
		if processErr != nil {
			out.Processing = "unavailable"
		}
		current, err = s.readCommitted(ctx, principal, stored)
		if err != nil {
			out.Status = "unknown"
			out.Verified = false
			out.Processing = "verification_unavailable"
			return out, nil
		}
		out.Status = current.Status
	}
	active, err := s.activeDeployment(ctx, nil, project.ID)
	if err == nil {
		isActive := active == stored.DeploymentID
		out.Active = &isActive
	}
	return out, nil
}

func (s *DeploymentRedeployService) readCommitted(ctx context.Context, principal Principal, stored deploymentRedeployReceipt) (*gendeployments.GetDeploymentResult, error) {
	result, err := s.management.GetDeployment(ctx, &gendeployments.GetDeploymentPayload{ID: stored.DeploymentID, ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return nil, fmt.Errorf("verify redeployment: %w", err)
	}
	if result == nil || result.ID != stored.DeploymentID || result.ProjectID != stored.ProjectID || result.OrganizationID != principal.OrganizationID || result.ClonedFrom == nil || *result.ClonedFrom != stored.SourceDeploymentID {
		return nil, fmt.Errorf("%w: redeployment verification mismatch", ErrUnavailable)
	}
	return result, nil
}
