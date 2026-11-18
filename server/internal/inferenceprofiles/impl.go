// Package inferenceprofiles implements the inferenceProfiles management API:
// organization-scoped inference endpoint profiles and the rules binding them
// to principals, resolved per harness into the device agent's tool_config
// (device-agent ADR-0032).
package inferenceprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/inference_profiles/server"
	gen "github.com/speakeasy-api/gram/server/gen/inference_profiles"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/inferenceprofiles/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	profileNameKey = "inference_profiles_organization_id_name_key"
	rulePrincipal  = "inference_profile_rules_org_principal_harness_key"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger) *Service {
	logger = logger.With(attr.SlogComponent("inferenceprofiles"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/inferenceprofiles"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessionManager, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// requireAdmin returns the caller's auth context once they hold org:admin.
func (s *Service) requireAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Org(authCtx.ActiveOrganizationID).Admin()); err != nil {
		return nil, err
	}
	return authCtx, nil
}

func actor(authCtx *contextvalues.AuthContext) urn.Principal {
	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
}

func (s *Service) ListProfiles(ctx context.Context, _ *gen.ListProfilesPayload) (*gen.ListInferenceProfilesResult, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListInferenceProfiles(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list inference profiles").LogError(ctx, s.logger)
	}
	profiles := make([]*gen.InferenceProfile, 0, len(rows))
	for _, row := range rows {
		view, err := buildProfileView(row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode inference profile").LogError(ctx, s.logger)
		}
		profiles = append(profiles, view)
	}
	return &gen.ListInferenceProfilesResult{Profiles: profiles}, nil
}

func (s *Service) CreateProfile(ctx context.Context, payload *gen.CreateProfilePayload) (*gen.InferenceProfile, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	name, settingsJSON, err := validatedProfileInput(payload.Name, payload.Provider, payload.Settings)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin inference profile create").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).CreateInferenceProfile(ctx, repo.CreateInferenceProfileParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
		Provider:       payload.Provider,
		Settings:       settingsJSON,
	})
	if err != nil {
		return nil, s.mapWriteError(ctx, err, "create inference profile")
	}
	view, err := buildProfileView(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode inference profile").LogError(ctx, s.logger)
	}
	if err := s.audit.LogInferenceProfileCreate(ctx, dbtx, audit.LogInferenceProfileCreateEvent{
		OrganizationID:                authCtx.ActiveOrganizationID,
		Actor:                         actor(authCtx),
		ActorDisplayName:              authCtx.Email,
		ActorSlug:                     nil,
		InferenceProfileURN:           urn.NewInferenceProfile(row.ID),
		ProfileName:                   row.Name,
		InferenceProfileSnapshotAfter: view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log inference profile create").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit inference profile create").LogError(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) UpdateProfile(ctx context.Context, payload *gen.UpdateProfilePayload) (*gen.InferenceProfile, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(payload.ID, "profile")
	if err != nil {
		return nil, err
	}
	name, settingsJSON, err := validatedProfileInput(payload.Name, payload.Provider, payload.Settings)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin inference profile update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetInferenceProfileForUpdate(ctx, repo.GetInferenceProfileForUpdateParams{OrganizationID: authCtx.ActiveOrganizationID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "inference profile not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get inference profile").LogError(ctx, s.logger)
	}

	// A provider change must not strand a harness-specific rule on a provider
	// its harness cannot use.
	if payload.Provider != before.Provider {
		rules, err := queries.ListInferenceProfileRulesForProfile(ctx, repo.ListInferenceProfileRulesForProfileParams{OrganizationID: authCtx.ActiveOrganizationID, ProfileID: id})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list inference profile rules").LogError(ctx, s.logger)
		}
		for _, rule := range rules {
			if err := ValidateHarness(rule.Harness.String, payload.Provider); err != nil {
				return nil, oops.E(oops.CodeBadRequest, nil, "%s: a rule for %s uses this profile; remove or change it first", err.Error(), rule.PrincipalUrn)
			}
		}
	}

	beforeView, err := buildProfileView(before)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode inference profile").LogError(ctx, s.logger)
	}
	row, err := queries.UpdateInferenceProfile(ctx, repo.UpdateInferenceProfileParams{
		Name:           name,
		Provider:       payload.Provider,
		Settings:       settingsJSON,
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             id,
	})
	if err != nil {
		return nil, s.mapWriteError(ctx, err, "update inference profile")
	}
	view, err := buildProfileView(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode inference profile").LogError(ctx, s.logger)
	}
	if err := s.audit.LogInferenceProfileUpdate(ctx, dbtx, audit.LogInferenceProfileUpdateEvent{
		OrganizationID:                 authCtx.ActiveOrganizationID,
		Actor:                          actor(authCtx),
		ActorDisplayName:               authCtx.Email,
		ActorSlug:                      nil,
		InferenceProfileURN:            urn.NewInferenceProfile(row.ID),
		ProfileName:                    row.Name,
		InferenceProfileSnapshotBefore: beforeView,
		InferenceProfileSnapshotAfter:  view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log inference profile update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit inference profile update").LogError(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) DeleteProfile(ctx context.Context, payload *gen.DeleteProfilePayload) error {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return err
	}
	id, err := parseID(payload.ID, "profile")
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin inference profile delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	row, err := queries.DeleteInferenceProfile(ctx, repo.DeleteInferenceProfileParams{OrganizationID: authCtx.ActiveOrganizationID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "inference profile not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "delete inference profile").LogError(ctx, s.logger)
	}

	// Soft deletes do not cascade, so the profile's rules go with it, one
	// audit entry each, before the profile's own entry.
	rules, err := queries.DeleteInferenceProfileRulesForProfile(ctx, repo.DeleteInferenceProfileRulesForProfileParams{OrganizationID: authCtx.ActiveOrganizationID, ProfileID: id})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete inference profile rules").LogError(ctx, s.logger)
	}
	for _, rule := range rules {
		if err := s.audit.LogInferenceProfileRuleDelete(ctx, dbtx, audit.LogInferenceProfileRuleDeleteEvent{
			OrganizationID:                     authCtx.ActiveOrganizationID,
			Actor:                              actor(authCtx),
			ActorDisplayName:                   authCtx.Email,
			ActorSlug:                          nil,
			InferenceProfileRuleURN:            urn.NewInferenceProfileRule(rule.ID),
			RuleDisplayName:                    rule.PrincipalUrn,
			InferenceProfileRuleSnapshotBefore: buildRuleView(rule),
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log inference profile rule delete").LogError(ctx, s.logger)
		}
	}

	view, err := buildProfileView(row)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "decode inference profile").LogError(ctx, s.logger)
	}
	if err := s.audit.LogInferenceProfileDelete(ctx, dbtx, audit.LogInferenceProfileDeleteEvent{
		OrganizationID:                 authCtx.ActiveOrganizationID,
		Actor:                          actor(authCtx),
		ActorDisplayName:               authCtx.Email,
		ActorSlug:                      nil,
		InferenceProfileURN:            urn.NewInferenceProfile(row.ID),
		ProfileName:                    row.Name,
		InferenceProfileSnapshotBefore: view,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log inference profile delete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit inference profile delete").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) ListRules(ctx context.Context, _ *gen.ListRulesPayload) (*gen.ListInferenceProfileRulesResult, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListInferenceProfileRules(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list inference profile rules").LogError(ctx, s.logger)
	}
	rules := make([]*gen.InferenceProfileRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, buildRuleView(row))
	}
	return &gen.ListInferenceProfileRulesResult{Rules: rules}, nil
}

type ruleInput struct {
	profileID uuid.UUID
	principal string
	harness   pgtype.Text
	position  int32
}

// validatedRuleInput checks a rule against its (locked) profile and
// normalizes the principal.
func (s *Service) validatedRuleInput(ctx context.Context, queries *repo.Queries, organizationID, rawProfileID, rawPrincipal string, harness *string, position int) (ruleInput, error) {
	var zero ruleInput
	profileID, err := parseID(rawProfileID, "profile")
	if err != nil {
		return zero, err
	}
	if position < 0 || position > 1_000_000 {
		return zero, oops.E(oops.CodeBadRequest, nil, "position must be between 0 and 1000000")
	}
	h := conv.PtrValOr(harness, "")
	profile, err := queries.GetInferenceProfileForUpdate(ctx, repo.GetInferenceProfileForUpdateParams{OrganizationID: organizationID, ID: profileID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, oops.E(oops.CodeBadRequest, nil, "inference profile not found")
	case err != nil:
		return zero, oops.E(oops.CodeUnexpected, err, "get inference profile").LogError(ctx, s.logger)
	}
	if err := ValidateHarness(h, profile.Provider); err != nil {
		return zero, oops.E(oops.CodeBadRequest, nil, "%s", err.Error())
	}
	principal, err := s.normalizePrincipal(ctx, organizationID, rawPrincipal)
	if err != nil {
		return zero, err
	}
	return ruleInput{
		profileID: profileID,
		principal: principal,
		harness:   pgtype.Text{String: h, Valid: h != ""},
		position:  int32(position),
	}, nil
}

// normalizePrincipal accepts the principal kinds plugin delivery resolves for
// a person and returns its canonical form.
func (s *Service) normalizePrincipal(ctx context.Context, organizationID, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case directory.IsGroupPrincipal(raw):
		id, err := directory.ParseGroupPrincipal(raw)
		if err != nil {
			return "", oops.E(oops.CodeBadRequest, err, "invalid directory group principal")
		}
		return directory.GroupPrincipal(id), nil
	case directory.IsAttributePrincipal(raw):
		value, err := directory.ParseAttributePrincipal(raw)
		if err != nil {
			return "", oops.E(oops.CodeBadRequest, err, "invalid directory attribute principal")
		}
		return directory.AttributePrincipal(value.Key, value.Value), nil
	}

	// Delivery matches the normalized address, so store it that way.
	if address, ok := strings.CutPrefix(raw, string(urn.PrincipalTypeEmail)+":"); ok {
		normalized, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + conv.NormalizeEmail(address))
		if err != nil {
			return "", oops.E(oops.CodeBadRequest, err, "invalid email principal")
		}
		return normalized.String(), nil
	}

	principal, err := urn.ParsePrincipal(raw)
	if err != nil {
		return "", oops.E(oops.CodeBadRequest, err, "invalid principal %q", raw)
	}
	switch principal.Type {
	case urn.PrincipalTypeUser, urn.PrincipalTypeRole:
		if err := authz.ValidatePrincipal(ctx, s.db, organizationID, principal); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return "", oops.E(oops.CodeBadRequest, err, "unknown principal %q", raw)
			}
			return "", oops.E(oops.CodeUnexpected, err, "validate principal").LogError(ctx, s.logger)
		}
		return principal.String(), nil
	default:
		return "", oops.E(oops.CodeBadRequest, nil, "inference profile rules apply to users, emails, roles, directory groups and attributes, or user:all")
	}
}

func (s *Service) CreateRule(ctx context.Context, payload *gen.CreateRulePayload) (*gen.InferenceProfileRule, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin inference profile rule create").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	input, err := s.validatedRuleInput(ctx, queries, authCtx.ActiveOrganizationID, payload.ProfileID, payload.PrincipalUrn, payload.Harness, payload.Position)
	if err != nil {
		return nil, err
	}
	row, err := queries.CreateInferenceProfileRule(ctx, repo.CreateInferenceProfileRuleParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProfileID:      input.profileID,
		PrincipalUrn:   input.principal,
		Harness:        input.harness,
		Position:       input.position,
	})
	if err != nil {
		return nil, s.mapWriteError(ctx, err, "create inference profile rule")
	}
	view := buildRuleView(row)
	if err := s.audit.LogInferenceProfileRuleCreate(ctx, dbtx, audit.LogInferenceProfileRuleCreateEvent{
		OrganizationID:                    authCtx.ActiveOrganizationID,
		Actor:                             actor(authCtx),
		ActorDisplayName:                  authCtx.Email,
		ActorSlug:                         nil,
		InferenceProfileRuleURN:           urn.NewInferenceProfileRule(row.ID),
		RuleDisplayName:                   row.PrincipalUrn,
		InferenceProfileRuleSnapshotAfter: view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log inference profile rule create").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit inference profile rule create").LogError(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) UpdateRule(ctx context.Context, payload *gen.UpdateRulePayload) (*gen.InferenceProfileRule, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(payload.ID, "rule")
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin inference profile rule update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	before, err := queries.GetInferenceProfileRuleForUpdate(ctx, repo.GetInferenceProfileRuleForUpdateParams{OrganizationID: authCtx.ActiveOrganizationID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "inference profile rule not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get inference profile rule").LogError(ctx, s.logger)
	}
	input, err := s.validatedRuleInput(ctx, queries, authCtx.ActiveOrganizationID, payload.ProfileID, payload.PrincipalUrn, payload.Harness, payload.Position)
	if err != nil {
		return nil, err
	}
	row, err := queries.UpdateInferenceProfileRule(ctx, repo.UpdateInferenceProfileRuleParams{
		ProfileID:      input.profileID,
		PrincipalUrn:   input.principal,
		Harness:        input.harness,
		Position:       input.position,
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             id,
	})
	if err != nil {
		return nil, s.mapWriteError(ctx, err, "update inference profile rule")
	}
	view := buildRuleView(row)
	if err := s.audit.LogInferenceProfileRuleUpdate(ctx, dbtx, audit.LogInferenceProfileRuleUpdateEvent{
		OrganizationID:                     authCtx.ActiveOrganizationID,
		Actor:                              actor(authCtx),
		ActorDisplayName:                   authCtx.Email,
		ActorSlug:                          nil,
		InferenceProfileRuleURN:            urn.NewInferenceProfileRule(row.ID),
		RuleDisplayName:                    row.PrincipalUrn,
		InferenceProfileRuleSnapshotBefore: buildRuleView(before),
		InferenceProfileRuleSnapshotAfter:  view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log inference profile rule update").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit inference profile rule update").LogError(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) DeleteRule(ctx context.Context, payload *gen.DeleteRulePayload) error {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return err
	}
	id, err := parseID(payload.ID, "rule")
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin inference profile rule delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).DeleteInferenceProfileRule(ctx, repo.DeleteInferenceProfileRuleParams{OrganizationID: authCtx.ActiveOrganizationID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "inference profile rule not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "delete inference profile rule").LogError(ctx, s.logger)
	}
	if err := s.audit.LogInferenceProfileRuleDelete(ctx, dbtx, audit.LogInferenceProfileRuleDeleteEvent{
		OrganizationID:                     authCtx.ActiveOrganizationID,
		Actor:                              actor(authCtx),
		ActorDisplayName:                   authCtx.Email,
		ActorSlug:                          nil,
		InferenceProfileRuleURN:            urn.NewInferenceProfileRule(row.ID),
		RuleDisplayName:                    row.PrincipalUrn,
		InferenceProfileRuleSnapshotBefore: buildRuleView(row),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log inference profile rule delete").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit inference profile rule delete").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) Preview(ctx context.Context, payload *gen.PreviewPayload) (*gen.PreviewInferenceProfilesResult, error) {
	authCtx, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	email := conv.NormalizeEmail(payload.Email)
	if _, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + email); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
	}
	// The same principals agent.getPlugins resolves for this person.
	principals, err := plugins.ResolveDeliveryPrincipals(ctx, s.db, authCtx.ActiveOrganizationID, email, "")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve principals").LogError(ctx, s.logger)
	}
	resolutions, err := ResolveForPrincipals(ctx, s.db, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve inference profiles").LogError(ctx, s.logger)
	}
	return buildPreviewView(resolutions), nil
}

func validatedProfileInput(rawName, provider string, settings *gen.InferenceProfileSettings) (string, []byte, error) {
	name := strings.TrimSpace(rawName)
	if name == "" || len([]rune(name)) > 100 {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "name must be 1 to 100 characters")
	}
	decoded := settingsFromView(settings)
	if err := ValidateProfile(provider, decoded); err != nil {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "%s", err.Error())
	}
	settingsJSON, err := json.Marshal(decoded)
	if err != nil {
		return "", nil, fmt.Errorf("encode inference profile settings: %w", err)
	}
	return name, settingsJSON, nil
}

func parseID(raw, kind string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid %s id", kind)
	}
	return id, nil
}

func (s *Service) mapWriteError(ctx context.Context, err error, action string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		switch pgErr.ConstraintName {
		case profileNameKey:
			return oops.E(oops.CodeConflict, nil, "an inference profile with this name already exists")
		case rulePrincipal:
			return oops.E(oops.CodeConflict, nil, "a rule for this principal and harness already exists")
		}
	}
	return oops.E(oops.CodeUnexpected, err, "%s", action).LogError(ctx, s.logger)
}
