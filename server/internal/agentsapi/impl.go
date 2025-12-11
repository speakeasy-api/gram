package agentsapi

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	commonv1 "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/agents"
	srv "github.com/speakeasy-api/gram/server/gen/http/agents/server"
	agentspkg "github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ agents.Service = (*Service)(nil)

type Service struct {
	tracer            trace.Tracer
	logger            *slog.Logger
	agentsService     *agentspkg.Service
	db                *pgxpool.Pool
	agentExecRepo     *repo.Queries
	auth              *auth.Auth
	temporalClient    client.Client
	temporalNamespace string // TODO: build a wrapper around temporal client to better encapsulate metadata like this
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	env *environments.EnvironmentEntries,
	enc *encryption.Client,
	cacheImpl cache.Cache,
	guardianPolicy *guardian.Policy,
	funcCaller functions.ToolCaller,
	openRouter openrouter.Provisioner,
	baseChatClient *openrouter.ChatClient,
	authService *auth.Auth,
	temporalClient client.Client,
	temporalNamespace string,
) *Service {
	logger = logger.With(attr.SlogComponent("agents-api"))

	agentsService := agentspkg.NewService(
		logger,
		tracerProvider,
		meterProvider,
		db,
		env,
		enc,
		cacheImpl,
		guardianPolicy,
		funcCaller,
		openRouter,
		baseChatClient,
	)

	return &Service{
		tracer:            otel.Tracer("github.com/speakeasy-api/gram/server/internal/agentsapi"),
		logger:            logger,
		agentsService:     agentsService,
		db:                db,
		agentExecRepo:     repo.New(db),
		auth:              authService,
		temporalClient:    temporalClient,
		temporalNamespace: temporalNamespace,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := agents.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) CreateResponse(ctx context.Context, payload *agents.CreateResponsePayload) (*agents.AgentResponseOutput, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	request, err := toServiceRequest(payload)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid request").Log(ctx, s.logger)
	}

	shouldStore := request.Store == nil || *request.Store
	isAsync := request.Async != nil && *request.Async

	if isAsync && !shouldStore {
		return nil, oops.E(oops.CodeBadRequest, nil, "async responses cannot have non stored agent history")
	}

	workflowRun, err := background.ExecuteAgentsResponseWorkflow(ctx, s.temporalClient, background.AgentsResponseWorkflowParams{
		OrgID:       authCtx.ActiveOrganizationID,
		ProjectID:   *authCtx.ProjectID,
		Request:     request,
		ShouldStore: shouldStore,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to start workflow").Log(ctx, s.logger)
	}

	if isAsync {
		// Return immediately with workflow ID and in-progress status
		return &agents.AgentResponseOutput{
			ID:                 workflowRun.GetID(),
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "in_progress",
			Error:              nil,
			Instructions:       request.Instructions,
			Model:              request.Model,
			Output:             []any{},
			PreviousResponseID: request.PreviousResponseID,
			Temperature:        getTemperature(request.Temperature),
			Text: &agents.AgentResponseText{
				Format: &agents.AgentTextFormat{Type: "text"},
			},
			Result: "",
		}, nil
	}

	// Wait for workflow to complete (synchronous mode)
	var workflowResult agentspkg.AgentsResponseWorkflowResult
	if err := workflowRun.Get(ctx, &workflowResult); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "workflow execution failed").Log(ctx, s.logger)
	}

	if authCtx.ProjectID == nil || workflowResult.ProjectID != *authCtx.ProjectID {
		return nil, oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
	}

	if !shouldStore {
		// Delete the workflow execution to remove history
		// No DB entry was written, so skip DB deletion
		go func() {
			if delErr := s.deleteAgentRun(context.WithoutCancel(ctx), workflowRun.GetID(), false); delErr != nil {
				s.logger.ErrorContext(ctx, "failed to delete non-stored agent run", attr.SlogError(delErr))
			}
		}()
	}

	return toHTTPResponse(workflowResult.ResponseOutput), nil
}

func (s *Service) GetResponse(ctx context.Context, payload *agents.GetResponsePayload) (*agents.AgentResponseOutput, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	responseID := payload.ResponseID

	desc, err := s.temporalClient.DescribeWorkflowExecution(ctx, responseID, "")
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
	}

	workflowStatus := desc.WorkflowExecutionInfo.Status

	var response *agents.AgentResponseOutput

	switch workflowStatus {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		// Query workflow for project_id and request parameters (only available while running)
		var projectID uuid.UUID
		queryValue, queryErr := s.temporalClient.QueryWorkflow(ctx, responseID, "", "project_id")
		if queryErr != nil {
			return nil, oops.E(oops.CodeNotFound, queryErr, "workflow not found").Log(ctx, s.logger)
		}
		if err := queryValue.Get(&projectID); err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
		}
		if authCtx.ProjectID == nil || projectID != *authCtx.ProjectID {
			return nil, oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
		}

		var requestParams agentspkg.ResponseRequest
		queryValue, queryErr = s.temporalClient.QueryWorkflow(ctx, responseID, "", "request")
		if queryErr != nil {
			s.logger.DebugContext(ctx, "failed to query workflow request parameters", attr.SlogError(queryErr))
		} else if err := queryValue.Get(&requestParams); err != nil {
			s.logger.DebugContext(ctx, "failed to decode workflow request parameters", attr.SlogError(err))
		}

		response = &agents.AgentResponseOutput{
			ID:                 responseID,
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "in_progress",
			Error:              nil,
			Instructions:       requestParams.Instructions,
			Model:              requestParams.Model,
			Output:             []any{},
			PreviousResponseID: requestParams.PreviousResponseID,
			Temperature:        getTemperature(requestParams.Temperature),
			Text: &agents.AgentResponseText{
				Format: &agents.AgentTextFormat{Type: "text"},
			},
			Result: "",
		}
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		// Workflow is complete, get the result which contains project_id and all request params
		workflowRun := s.temporalClient.GetWorkflow(ctx, responseID, "")
		var workflowResult agentspkg.AgentsResponseWorkflowResult
		err = workflowRun.Get(ctx, &workflowResult)
		if err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
		}

		if authCtx.ProjectID == nil || workflowResult.ProjectID != *authCtx.ProjectID {
			return nil, oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
		}
		response = toHTTPResponse(workflowResult.ResponseOutput)
	default:
		// Workflow failed, cancelled, or terminated - try to get result for any available data
		workflowRun := s.temporalClient.GetWorkflow(ctx, responseID, "")
		var workflowResult agentspkg.AgentsResponseWorkflowResult
		err = workflowRun.Get(ctx, &workflowResult)
		if err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
		}

		if authCtx.ProjectID == nil || workflowResult.ProjectID != *authCtx.ProjectID {
			return nil, oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
		}

		errMsg := fmt.Sprintf("workflow in unexpected state: %v", workflowStatus)
		response = toHTTPResponse(workflowResult.ResponseOutput)
		response.Status = "failed"
		response.Error = &errMsg
	}

	return response, nil
}

func (s *Service) DeleteResponse(ctx context.Context, payload *agents.DeleteResponsePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	responseID := payload.ResponseID

	desc, err := s.temporalClient.DescribeWorkflowExecution(ctx, responseID, "")
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
	}

	workflowStatus := desc.WorkflowExecutionInfo.Status

	// Verify ownership based on workflow status
	switch workflowStatus {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		var projectID uuid.UUID
		queryValue, queryErr := s.temporalClient.QueryWorkflow(ctx, responseID, "", "project_id")
		if queryErr != nil {
			return oops.E(oops.CodeNotFound, queryErr, "workflow not found").Log(ctx, s.logger)
		}
		if err := queryValue.Get(&projectID); err != nil {
			return oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
		}
		if authCtx.ProjectID == nil || projectID != *authCtx.ProjectID {
			return oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
		}
	default:
		// For failed, cancelled, or terminated workflows, try to get result
		workflowRun := s.temporalClient.GetWorkflow(ctx, responseID, "")
		var workflowResult agentspkg.AgentsResponseWorkflowResult
		if err := workflowRun.Get(ctx, &workflowResult); err != nil {
			// Cannot verify ownership, deny access
			return oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
		}
		if authCtx.ProjectID == nil || workflowResult.ProjectID != *authCtx.ProjectID {
			return oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
		}
	}

	return s.deleteAgentRun(ctx, responseID, true)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) deleteAgentRun(ctx context.Context, responseID string, deleteDBEntry bool) error {
	// Delete database entry if it exists (soft delete)
	if deleteDBEntry {
		if err := s.agentExecRepo.DeleteAgentExecution(ctx, responseID); err != nil {
			// Log but don't fail if DB entry doesn't exist
			s.logger.DebugContext(ctx, "failed to delete agent execution from database", attr.SlogError(err))
		}
	}

	// Delete workflow execution
	_, err := s.temporalClient.WorkflowService().DeleteWorkflowExecution(ctx, &workflowservice.DeleteWorkflowExecutionRequest{
		Namespace: s.temporalNamespace,
		WorkflowExecution: &commonv1.WorkflowExecution{
			WorkflowId: responseID,
			RunId:      "",
		},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete agent run").Log(ctx, s.logger)
	}
	return nil
}

func getTemperature(temp *float64) float64 {
	if temp != nil {
		return *temp
	}
	return 0.5
}

// toInternalRequest converts a Goa request to the internal request type.
func toServiceRequest(req *agents.CreateResponsePayload) (agentspkg.ResponseRequest, error) {
	var toolsets []agentspkg.Toolset
	for _, ts := range req.Toolsets {
		toolsets = append(toolsets, agentspkg.Toolset{
			ToolsetSlug:     ts.ToolsetSlug,
			EnvironmentSlug: ts.EnvironmentSlug,
		})
	}

	subAgentNames := make(map[string]bool)
	var subAgents []agentspkg.SubAgent
	for _, sa := range req.SubAgents {
		if subAgentNames[sa.Name] {
			return agentspkg.ResponseRequest{}, fmt.Errorf("duplicate sub-agent name: %q", sa.Name)
		}
		subAgentNames[sa.Name] = true

		var saToolsets []agentspkg.Toolset
		for _, ts := range sa.Toolsets {
			saToolsets = append(saToolsets, agentspkg.Toolset{
				ToolsetSlug:     ts.ToolsetSlug,
				EnvironmentSlug: ts.EnvironmentSlug,
			})
		}

		var tools []urn.Tool
		for _, t := range sa.Tools {
			toolURN, err := urn.ParseTool(t)
			if err == nil {
				tools = append(tools, toolURN)
			}
		}

		var envSlug string
		if sa.EnvironmentSlug != nil {
			envSlug = *sa.EnvironmentSlug
		}

		var instructions string
		if sa.Instructions != nil {
			instructions = *sa.Instructions
		}

		subAgents = append(subAgents, agentspkg.SubAgent{
			Instructions:    instructions,
			Name:            sa.Name,
			Description:     sa.Description,
			Tools:           tools,
			Toolsets:        saToolsets,
			EnvironmentSlug: envSlug,
		})
	}

	return agentspkg.ResponseRequest{
		Model:              req.Model,
		Instructions:       req.Instructions,
		Input:              req.Input,
		PreviousResponseID: req.PreviousResponseID,
		Temperature:        req.Temperature,
		Toolsets:           toolsets,
		SubAgents:          subAgents,
		Async:              req.Async,
		Store:              req.Store,
	}, nil
}

// toGoaResponse converts an internal response to the Goa response type.
func toHTTPResponse(resp agentspkg.ResponseOutput) *agents.AgentResponseOutput {
	var output []any
	for _, item := range resp.Output {
		output = append(output, item)
	}

	return &agents.AgentResponseOutput{
		ID:                 resp.ID,
		Object:             resp.Object,
		CreatedAt:          resp.CreatedAt,
		Status:             resp.Status,
		Error:              resp.Error,
		Instructions:       resp.Instructions,
		Model:              resp.Model,
		Output:             output,
		PreviousResponseID: resp.PreviousResponseID,
		Temperature:        resp.Temperature,
		Text: &agents.AgentResponseText{
			Format: &agents.AgentTextFormat{Type: resp.Text.Format.Type},
		},
		Result: resp.Result,
	}
}
