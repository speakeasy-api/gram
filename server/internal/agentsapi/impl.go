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
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/agents"
	srv "github.com/speakeasy-api/gram/server/gen/http/agents/server"
	agentspkg "github.com/speakeasy-api/gram/server/internal/agents"
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
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ agents.Service = (*Service)(nil)

type Service struct {
	tracer         trace.Tracer
	logger         *slog.Logger
	agentsService  *agentspkg.Service
	db             *pgxpool.Pool
	auth           *auth.Auth
	temporalClient client.Client
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
) *Service {
	logger = logger.With(attr.SlogComponent("agents-api"))

	// Create the agents service
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
		tracer:         otel.Tracer("github.com/speakeasy-api/gram/server/internal/agentsapi"),
		logger:         logger,
		agentsService:  agentsService,
		db:             db,
		auth:           authService,
		temporalClient: temporalClient,
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

// CreateResponse implements agents.Service.
func (s *Service) CreateResponse(ctx context.Context, payload *agents.CreateResponsePayload) (*agents.AgentResponseOutput, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if payload.Body == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "request body is required")
	}

	request := toServiceRequest(payload.Body)

	s.logger.InfoContext(ctx, "agents response request received",
		attr.SlogProjectSlug(request.ProjectSlug))

	// Look up project by slug within the organization
	projectsRepo := projects_repo.New(s.db)
	projects, err := projectsRepo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list projects").Log(ctx, s.logger)
	}

	var projectID uuid.UUID
	for _, proj := range projects {
		if proj.Slug == request.ProjectSlug {
			projectID = proj.ID
			break
		}
	}

	if projectID == uuid.Nil {
		return nil, oops.E(oops.CodeNotFound, fmt.Errorf("project not found"), "project not found").Log(ctx, s.logger)
	}

	// Execute workflow
	workflowRun, err := background.ExecuteAgentsResponseWorkflow(ctx, s.temporalClient, background.AgentsResponseWorkflowParams{
		OrgID:     authCtx.ActiveOrganizationID,
		ProjectID: projectID,
		Request:   request,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to start workflow").Log(ctx, s.logger)
	}

	// Check if request is async
	isAsync := request.Async != nil && *request.Async

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

	return toHTTPResponse(workflowResult.ResponseOutput), nil
}

// GetResponse implements agents.Service.
func (s *Service) GetResponse(ctx context.Context, payload *agents.GetResponsePayload) (*agents.AgentResponseOutput, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	responseID := payload.ResponseID

	s.logger.InfoContext(ctx, "agents response status query",
		slog.String("response_id", responseID)) //nolint:sloglint // response_id is a valid key for this context

	// Describe workflow to check status without blocking
	desc, err := s.temporalClient.DescribeWorkflowExecution(ctx, responseID, "")
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
	}

	// Check workflow status
	workflowStatus := desc.WorkflowExecutionInfo.Status

	var response *agents.AgentResponseOutput

	switch workflowStatus {
	case 1: // Running
		// Query workflow for org_id and request parameters (only available while running)
		var orgID string
		queryValue, queryErr := s.temporalClient.QueryWorkflow(ctx, responseID, "", "org_id")
		if queryErr != nil {
			return nil, oops.E(oops.CodeNotFound, queryErr, "workflow not found").Log(ctx, s.logger)
		}
		if err := queryValue.Get(&orgID); err != nil {
			return nil, oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
		}
		if orgID != authCtx.ActiveOrganizationID {
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
	case 2: // Completed
		// Workflow is complete, get the result which contains org_id and all request params
		workflowRun := s.temporalClient.GetWorkflow(ctx, responseID, "")
		var workflowResult agentspkg.AgentsResponseWorkflowResult
		err = workflowRun.Get(ctx, &workflowResult)
		if err != nil {
			errMsg := err.Error()
			response = &agents.AgentResponseOutput{
				ID:                 responseID,
				Object:             "response",
				CreatedAt:          time.Now().Unix(),
				Status:             "failed",
				Error:              &errMsg,
				Instructions:       nil,
				Model:              "",
				Output:             []any{},
				PreviousResponseID: nil,
				Temperature:        0,
				Text: &agents.AgentResponseText{
					Format: &agents.AgentTextFormat{Type: "text"},
				},
				Result: errMsg,
			}
		} else {
			// Verify org_id matches
			if workflowResult.OrgID != authCtx.ActiveOrganizationID {
				return nil, oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
			}
			response = toHTTPResponse(workflowResult.ResponseOutput)
		}
	default:
		// Workflow failed, cancelled, or terminated - try to get result for any available data
		workflowRun := s.temporalClient.GetWorkflow(ctx, responseID, "")
		var workflowResult agentspkg.AgentsResponseWorkflowResult
		err = workflowRun.Get(ctx, &workflowResult)

		errMsg := fmt.Sprintf("workflow in unexpected state: %v", workflowStatus)
		if err == nil && workflowResult.OrgID != "" {
			// Verify org_id matches
			if workflowResult.OrgID != authCtx.ActiveOrganizationID {
				return nil, oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
			}
			// Use data from workflow result if available
			response = toHTTPResponse(workflowResult.ResponseOutput)
			response.Status = "failed"
			response.Error = &errMsg
		} else {
			// Fallback to minimal response
			response = &agents.AgentResponseOutput{
				ID:                 responseID,
				Object:             "response",
				CreatedAt:          time.Now().Unix(),
				Status:             "failed",
				Error:              &errMsg,
				Instructions:       nil,
				Model:              "",
				Output:             []any{},
				PreviousResponseID: nil,
				Temperature:        0,
				Text: &agents.AgentResponseText{
					Format: &agents.AgentTextFormat{Type: "text"},
				},
				Result: "",
			}
		}
	}

	return response, nil
}

// APIKeyAuth implements agents.Auther.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func getTemperature(temp *float64) float64 {
	if temp != nil {
		return *temp
	}
	return 0.5
}

// toInternalRequest converts a Goa request to the internal request type.
func toServiceRequest(req *agents.AgentResponseRequest) agentspkg.ResponseRequest {
	var toolsets []agentspkg.Toolset
	for _, ts := range req.Toolsets {
		toolsets = append(toolsets, agentspkg.Toolset{
			ToolsetSlug:     ts.ToolsetSlug,
			EnvironmentSlug: ts.EnvironmentSlug,
		})
	}

	var subAgents []agentspkg.SubAgent
	for _, sa := range req.SubAgents {
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
		ProjectSlug:        req.ProjectSlug,
		Model:              req.Model,
		Instructions:       req.Instructions,
		Input:              req.Input,
		PreviousResponseID: req.PreviousResponseID,
		Temperature:        req.Temperature,
		Toolsets:           toolsets,
		SubAgents:          subAgents,
		Async:              req.Async,
	}
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
