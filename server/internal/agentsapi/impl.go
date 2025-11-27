package agentsapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type Service struct {
	logger         *slog.Logger
	agentsService  *agents.Service
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
	agentsService := agents.NewService(
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
		logger:         logger,
		agentsService:  agentsService,
		db:             db,
		auth:           authService,
		temporalClient: temporalClient,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	o11y.AttachHandler(mux, "POST", "/rpc/agents.response", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.HandleResponse).ServeHTTP(w, r)
	})

	o11y.AttachHandler(mux, "GET", "/rpc/agents.response", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.HandleGetResponse).ServeHTTP(w, r)
	})
}

// HandleResponse handles the /rpc/agents.response endpoint
// This endpoint accepts an OpenAI Responses API request and returns a response
func (s *Service) HandleResponse(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Read the request body
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, s.logger)
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			s.logger.ErrorContext(ctx, "failed to close request body", attr.SlogError(err))
		}
	}()

	// Parse the request
	var request agents.ResponseRequest
	if err := json.Unmarshal(reqBody, &request); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse request body").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "agents response request received",
		attr.SlogProjectSlug(request.ProjectSlug))

	authorizedCtx, err := s.auth.Authorize(ctx, r.Header.Get("Gram-Key"), &security.APIKeyScheme{
		Name:           auth.KeySecurityScheme,
		RequiredScopes: []string{"consumer"},
		Scopes:         []string{},
	})
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to authorize with API key").Log(ctx, s.logger)
	}

	authCtx, ok := contextvalues.GetAuthContext(authorizedCtx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return oops.E(oops.CodeUnauthorized, fmt.Errorf("no active organization"), "unauthorized").Log(ctx, s.logger)
	}

	// Look up project by slug within the organization
	projectsRepo := projects_repo.New(s.db)
	projects, err := projectsRepo.ListProjectsByOrganization(authorizedCtx, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to list projects").Log(ctx, s.logger)
	}

	var projectID uuid.UUID
	for _, proj := range projects {
		if proj.Slug == request.ProjectSlug {
			projectID = proj.ID
			break
		}
	}

	if projectID == uuid.Nil {
		return oops.E(oops.CodeNotFound, fmt.Errorf("project not found"), "project not found").Log(ctx, s.logger)
	}

	// Execute workflow
	workflowRun, err := background.ExecuteAgentsResponseWorkflow(authorizedCtx, s.temporalClient, background.AgentsResponseWorkflowParams{
		OrgID:     authCtx.ActiveOrganizationID,
		ProjectID: projectID,
		Request:   request,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to start workflow").Log(ctx, s.logger)
	}

	// Check if request is async
	isAsync := request.Async != nil && *request.Async

	if isAsync {
		// Return immediately with workflow ID and in-progress status
		response := agents.ResponseOutput{
			ID:                 workflowRun.GetID(),
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "in_progress",
			Error:              nil,
			Instructions:       request.Instructions,
			Model:              request.Model,
			Output:             []agents.OutputItem{},
			PreviousResponseID: request.PreviousResponseID,
			Temperature:        getTemperature(request.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Result: "",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to encode response").Log(ctx, s.logger)
		}
		return nil
	}

	// Wait for workflow to complete (synchronous mode)
	var workflowResult agents.AgentsResponseWorkflowResult
	if err := workflowRun.Get(authorizedCtx, &workflowResult); err != nil {
		return oops.E(oops.CodeUnexpected, err, "workflow execution failed").Log(ctx, s.logger)
	}

	// Write response (extract just ResponseOutput from wrapper)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(workflowResult.ResponseOutput); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to encode response").Log(ctx, s.logger)
	}

	return nil
}

// HandleGetResponse handles GET /rpc/agents.response?response_id=<id>
// This endpoint allows querying the status of an async agent response
func (s *Service) HandleGetResponse(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// Get response_id from query params
	responseID := r.URL.Query().Get("response_id")
	if responseID == "" {
		return oops.E(oops.CodeBadRequest, fmt.Errorf("missing response_id"), "response_id query parameter is required").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "agents response status query",
		slog.String("response_id", responseID)) //nolint:sloglint // response_id is a valid key for this context

	authorizedCtx, err := s.auth.Authorize(ctx, r.Header.Get("Gram-Key"), &security.APIKeyScheme{
		Name:           auth.KeySecurityScheme,
		RequiredScopes: []string{"consumer"},
		Scopes:         []string{},
	})
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to authorize with API key").Log(ctx, s.logger)
	}

	authCtx, ok := contextvalues.GetAuthContext(authorizedCtx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return oops.E(oops.CodeUnauthorized, fmt.Errorf("no active organization"), "unauthorized").Log(ctx, s.logger)
	}

	// Describe workflow to check status without blocking
	desc, err := s.temporalClient.DescribeWorkflowExecution(authorizedCtx, responseID, "")
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
	}

	// Check workflow status
	workflowStatus := desc.WorkflowExecutionInfo.Status

	// Check org id from query handler
	var orgID string
	queryValue, err := s.temporalClient.QueryWorkflow(authorizedCtx, responseID, "", "org_id")
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
	}
	if err := queryValue.Get(&orgID); err != nil {
		return oops.E(oops.CodeNotFound, err, "workflow not found").Log(ctx, s.logger)
	}
	if orgID != authCtx.ActiveOrganizationID {
		return oops.E(oops.CodeNotFound, fmt.Errorf("workflow not found"), "workflow not found").Log(ctx, s.logger)
	}

	// Query workflow for request parameters
	var requestParams agents.ResponseRequest
	queryValue, queryErr := s.temporalClient.QueryWorkflow(authorizedCtx, responseID, "", "request")
	if queryErr != nil {
		s.logger.DebugContext(ctx, "failed to query workflow request parameters", attr.SlogError(queryErr))
	} else if err := queryValue.Get(&requestParams); err != nil {
		s.logger.DebugContext(ctx, "failed to decode workflow request parameters", attr.SlogError(err))
	}

	var response agents.ResponseOutput

	switch workflowStatus {
	case 1: // Running
		response = agents.ResponseOutput{
			ID:                 responseID,
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "in_progress",
			Error:              nil,
			Instructions:       requestParams.Instructions,
			Model:              requestParams.Model,
			Output:             []agents.OutputItem{},
			PreviousResponseID: requestParams.PreviousResponseID,
			Temperature:        getTemperature(requestParams.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Result: "",
		}
	case 2: // Completed
		// Workflow is complete, get the result wrapper and extract ResponseOutput
		workflowRun := s.temporalClient.GetWorkflow(authorizedCtx, responseID, "")
		var workflowResult agents.AgentsResponseWorkflowResult
		err = workflowRun.Get(authorizedCtx, &workflowResult)
		if err != nil {
			errMsg := err.Error()
			response = agents.ResponseOutput{
				ID:                 responseID,
				Object:             "response",
				CreatedAt:          time.Now().Unix(),
				Status:             "failed",
				Error:              &errMsg,
				Instructions:       requestParams.Instructions,
				Model:              requestParams.Model,
				Output:             []agents.OutputItem{},
				PreviousResponseID: requestParams.PreviousResponseID,
				Temperature:        getTemperature(requestParams.Temperature),
				Text: agents.ResponseText{
					Format: agents.TextFormat{Type: "text"},
				},
				Result: errMsg,
			}
		} else {
			response = workflowResult.ResponseOutput
		}
	default:
		// Workflow failed, cancelled, or terminated
		errMsg := fmt.Sprintf("workflow in unexpected state: %v", workflowStatus)
		response = agents.ResponseOutput{
			ID:                 responseID,
			Object:             "response",
			CreatedAt:          time.Now().Unix(),
			Status:             "failed",
			Error:              &errMsg,
			Instructions:       requestParams.Instructions,
			Model:              requestParams.Model,
			Output:             []agents.OutputItem{},
			PreviousResponseID: requestParams.PreviousResponseID,
			Temperature:        getTemperature(requestParams.Temperature),
			Text: agents.ResponseText{
				Format: agents.TextFormat{Type: "text"},
			},
			Result: "",
		}
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to encode response").Log(ctx, s.logger)
	}

	return nil
}

func getTemperature(temp *float64) float64 {
	if temp != nil {
		return *temp
	}
	return 0.5
}
