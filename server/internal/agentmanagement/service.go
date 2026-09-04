package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	srv "github.com/speakeasy-api/gram/server/gen/http/agents/server"
	"github.com/speakeasy-api/gram/server/internal/agentownership"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type sessionAuthorizer interface {
	AuthorizeWithPostAuthenticationCheck(
		context.Context,
		string,
		*security.APIKeyScheme,
		func(context.Context) error,
	) (context.Context, error)
}

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	db         *pgxpool.Pool
	auth       sessionAuthorizer
	authorizer *Authorizer
	audit      *audit.Logger
	features   feature.Provider
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	features feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("agents"))
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/agents"),
		logger:     logger,
		db:         db,
		auth:       auth.New(logger, db, sessionManager, authzEngine),
		authorizer: NewAuthorizer(authzEngine),
		audit:      auditLogger,
		features:   features,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.AuthorizeWithPostAuthenticationCheck(ctx, key, schema, s.requireM1Enabled)
}

func (s *Service) requireM1Enabled(ctx context.Context) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeNotFound)
	}

	evaluation, err := feature.EvaluateFlag(
		ctx,
		s.features,
		feature.FlagAgentManagementM1,
		authCtx.ActiveOrganizationID,
		feature.OrgProjectGroups(authCtx.OrganizationSlug, ""),
	)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to evaluate agent management rollout flag", attr.SlogError(err))
	}
	if evaluation != feature.EvaluationEnabled {
		return oops.C(oops.CodeNotFound)
	}

	return nil
}

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*gen.ManagedAgent, error) {
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, err
	}
	agentID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate agent id").LogError(ctx, s.logger)
	}

	var result *gen.ManagedAgent
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		authCtx, ok := contextAuth(ctx)
		if !ok {
			return oops.C(oops.CodeUnauthorized)
		}
		ownerUserID := authCtx.UserID
		if payload.OwnerUserID != nil && *payload.OwnerUserID != "" {
			ownerUserID = *payload.OwnerUserID
		}

		human, err := s.authorizer.RequireCreate(ctx, tx, agentID, ownerUserID)
		if err != nil {
			return err
		}
		agent, err := repo.New(tx).CreateAgentWithID(ctx, repo.CreateAgentWithIDParams{
			ID:             agentID,
			OrganizationID: human.Auth.ActiveOrganizationID,
			OwnerUserID:    ownerUserID,
			Name:           name,
		})
		if err != nil {
			return mapWriteError(err, "agent name is already in use")
		}
		if err := s.logAgent(ctx, tx, human, audit.ActionAgentCreate, nil, &agent); err != nil {
			return err
		}
		result, err = s.view(ctx, human, agent)
		return err
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, "create agent")
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.ManagedAgent, error) {
	agentID, err := parseAgentID(payload.ID)
	if err != nil {
		return nil, err
	}
	human, agent, err := s.authorizer.RequireAgent(ctx, s.db, agentID, OwnedAgentRead)
	if err != nil {
		return nil, s.serviceError(ctx, err, "read agent")
	}
	result, err := s.view(ctx, human, agent)
	if err != nil {
		return nil, s.serviceError(ctx, err, "build agent response")
	}
	return result, nil
}

func (s *Service) Rename(ctx context.Context, payload *gen.RenamePayload) (*gen.ManagedAgent, error) {
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, payload.ID, audit.ActionAgentRename, func(ctx context.Context, q *repo.Queries, human HumanContext, before repo.Agent) (repo.Agent, error) {
		return q.RenameAgent(ctx, repo.RenameAgentParams{Name: name, OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID})
	})
}

func (s *Service) Transfer(ctx context.Context, payload *gen.TransferPayload) (*gen.ManagedAgent, error) {
	return s.changeOwner(ctx, payload.AgentID, payload.OwnerUserID, false)
}

func (s *Service) Reassign(ctx context.Context, payload *gen.ReassignPayload) (*gen.ManagedAgent, error) {
	return s.changeOwner(ctx, payload.AgentID, payload.OwnerUserID, true)
}

func (s *Service) changeOwner(ctx context.Context, rawID, ownerUserID string, explicitReassignment bool) (*gen.ManagedAgent, error) {
	agentID, err := parseAgentID(rawID)
	if err != nil {
		return nil, err
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "replacement owner is required")
	}

	action := audit.ActionAgentTransfer
	operation := "transfer agent"
	if explicitReassignment {
		action = audit.ActionAgentReassign
		operation = "reassign agent"
	}

	var result *gen.ManagedAgent
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, before, err := s.authorizer.RequireTransfer(ctx, tx, agentID, ownerUserID)
		if err != nil {
			return err
		}

		var after repo.Agent
		if explicitReassignment {
			after, err = repo.New(tx).ReassignAgent(ctx, repo.ReassignAgentParams{
				OwnerUserID: ownerUserID, OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID,
			})
		} else {
			after, err = repo.New(tx).TransferAgent(ctx, repo.TransferAgentParams{
				OwnerUserID: ownerUserID, OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID,
			})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeConflict, nil, "agent ownership transition is not allowed")
		}
		if err != nil {
			return fmt.Errorf("change agent owner: %w", err)
		}
		if err := s.logAgent(ctx, tx, human, action, &before, &after); err != nil {
			return err
		}
		result, err = s.view(ctx, human, after)
		return err
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, operation)
	}
	return result, nil
}

func (s *Service) Suspend(ctx context.Context, payload *gen.SuspendPayload) (*gen.ManagedAgent, error) {
	return s.mutate(ctx, payload.AgentID, audit.ActionAgentSuspend, func(ctx context.Context, q *repo.Queries, human HumanContext, before repo.Agent) (repo.Agent, error) {
		return q.SuspendAgent(ctx, repo.SuspendAgentParams{OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID})
	})
}

func (s *Service) Resume(ctx context.Context, payload *gen.ResumePayload) (*gen.ManagedAgent, error) {
	return s.mutate(ctx, payload.AgentID, audit.ActionAgentResume, func(ctx context.Context, q *repo.Queries, human HumanContext, before repo.Agent) (repo.Agent, error) {
		return q.ResumeAgent(ctx, repo.ResumeAgentParams{OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID})
	})
}

func (s *Service) Revoke(ctx context.Context, payload *gen.RevokePayload) (*gen.ManagedAgent, error) {
	return s.mutate(ctx, payload.AgentID, audit.ActionAgentRevoke, func(ctx context.Context, q *repo.Queries, human HumanContext, before repo.Agent) (repo.Agent, error) {
		return q.RevokeAgent(ctx, repo.RevokeAgentParams{OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID})
	})
}

func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) error {
	_, err := s.mutate(ctx, payload.AgentID, audit.ActionAgentDelete, func(ctx context.Context, q *repo.Queries, human HumanContext, before repo.Agent) (repo.Agent, error) {
		return q.DeleteAgent(ctx, repo.DeleteAgentParams{OrganizationID: human.Auth.ActiveOrganizationID, ID: before.ID})
	})
	return err
}

type agentMutation func(context.Context, *repo.Queries, HumanContext, repo.Agent) (repo.Agent, error)

func (s *Service) mutate(ctx context.Context, rawID string, action audit.Action, mutation agentMutation) (*gen.ManagedAgent, error) {
	agentID, err := parseAgentID(rawID)
	if err != nil {
		return nil, err
	}

	var result *gen.ManagedAgent
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, before, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, OwnedAgentSetup)
		if err != nil {
			return err
		}
		after, err := mutation(ctx, repo.New(tx), human, before)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeConflict, nil, "agent lifecycle transition is not allowed")
		}
		if err != nil {
			return mapWriteError(err, "agent name is already in use")
		}
		if err := s.logAgent(ctx, tx, human, action, &before, &after); err != nil {
			return err
		}
		result, err = s.view(ctx, human, after)
		return err
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, string(action))
	}
	return result, nil
}

func (s *Service) logAgent(ctx context.Context, tx pgx.Tx, human HumanContext, action audit.Action, before, after *repo.Agent) error {
	var beforeSnapshot *audit.AgentSnapshot
	if before != nil {
		beforeSnapshot = agentAuditSnapshot(*before)
	}
	afterSnapshot := agentAuditSnapshot(*after)
	if err := s.audit.LogAgent(ctx, tx, audit.LogAgentEvent{
		OrganizationID:   human.Auth.ActiveOrganizationID,
		AgentURN:         urn.NewAgentIdentity(after.ID.String()),
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID),
		ActorDisplayName: human.Auth.Email,
		Action:           action,
		Name:             after.Name,
		Before:           beforeSnapshot,
		After:            afterSnapshot,
	}); err != nil {
		return fmt.Errorf("log %s audit event: %w", action, err)
	}
	return nil
}

func (s *Service) view(ctx context.Context, human HumanContext, agent repo.Agent) (*gen.ManagedAgent, error) {
	permissions, err := s.authorizer.Permissions(ctx, human, agent)
	if err != nil {
		return nil, fmt.Errorf("evaluate agent permissions: %w", err)
	}
	result := &gen.ManagedAgent{
		ID:                          agent.ID.String(),
		OwnerUserID:                 agent.OwnerUserID,
		OwnerReassignmentRequiredAt: nil,
		OwnerReassignmentReason:     nil,
		Name:                        agent.Name,
		Lifecycle:                   gen.AgentLifecycle(agents.DeriveLifecycle(agent)),
		Permissions: &gen.AgentPermissions{
			Read:      permissions.Read,
			Write:     permissions.Write,
			Authorize: permissions.Authorize,
			Transfer:  permissions.Transfer,
		},
		CreatedAt: agent.CreatedAt.Time.Format(time.RFC3339Nano),
		UpdatedAt: agent.UpdatedAt.Time.Format(time.RFC3339Nano),
	}
	if agent.OwnerReassignmentRequiredAt.Valid {
		value := agent.OwnerReassignmentRequiredAt.Time.Format(time.RFC3339Nano)
		result.OwnerReassignmentRequiredAt = &value
	}
	if agent.OwnerReassignmentReason.Valid {
		value := agent.OwnerReassignmentReason.String
		result.OwnerReassignmentReason = &value
	}
	return result, nil
}

func agentAuditSnapshot(agent repo.Agent) *audit.AgentSnapshot {
	return agentownership.AgentAuditSnapshot(agent)
}

func parseAgentID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid agent id")
	}
	return id, nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", oops.E(oops.CodeBadRequest, nil, "agent name is required")
	}
	if len([]rune(name)) > 120 {
		return "", oops.E(oops.CodeBadRequest, nil, "agent name must be at most 120 characters")
	}
	return name, nil
}

func mapWriteError(err error, conflictMessage string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return oops.E(oops.CodeConflict, err, "%s", conflictMessage)
	}
	return err
}

func (s *Service) serviceError(ctx context.Context, err error, operation string) error {
	if _, ok := errors.AsType[*oops.ShareableError](err); ok {
		return err
	}
	return oops.E(oops.CodeUnexpected, err, "%s", operation).LogError(ctx, s.logger)
}

func contextAuth(ctx context.Context) (*contextvalues.AuthContext, bool) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	return authCtx, ok && authCtx != nil
}
