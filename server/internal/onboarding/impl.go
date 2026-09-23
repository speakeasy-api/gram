// Package onboarding is the organization onboarding service: the stack an
// admin describes, the one use case they pick, the one next step to get
// there, and verification of each step against real evidence.
package onboarding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/onboarding/server"
	gen "github.com/speakeasy-api/gram/server/gen/onboarding"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/onboarding/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Stage is where an organization is in onboarding.
type Stage string

const (
	// StageStack: the organization's stack has not been saved.
	StageStack Stage = "stack"
	// StageUseCase: the stack is saved and a use case is needed.
	StageUseCase Stage = "use-case"
	// StageSteps: a use case is picked and steps remain.
	StageSteps Stage = "steps"
	// StageDone: the use case is covered.
	StageDone Stage = "done"
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	audit    *audit.Logger
	evidence EvidenceReader
	catalog  Catalog
	now      func() time.Time
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, auditLogger *audit.Logger, evidence EvidenceReader) *Service {
	logger = logger.With(attr.SlogComponent("onboarding"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/onboarding"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		audit:    auditLogger,
		evidence: evidence,
		catalog:  Default,
		now:      time.Now,
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

func (s *Service) authorize(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

// referenceRows is the reference data read in one go.
type referenceRows struct {
	providers []repo.OnboardingProvider
	plans     []repo.ListOnboardingPlansRow
	products  []repo.ListOnboardingProductsRow
}

func loadReference(ctx context.Context, queries *repo.Queries) (referenceRows, error) {
	var rows referenceRows
	var err error
	if rows.providers, err = queries.ListOnboardingProviders(ctx); err != nil {
		return rows, fmt.Errorf("list onboarding providers: %w", err)
	}
	if rows.plans, err = queries.ListOnboardingPlans(ctx); err != nil {
		return rows, fmt.Errorf("list onboarding plans: %w", err)
	}
	if rows.products, err = queries.ListOnboardingProducts(ctx); err != nil {
		return rows, fmt.Errorf("list onboarding products: %w", err)
	}
	return rows, nil
}

// ListReferenceData returns the providers, plans and products from the
// reference tables plus the fixed use case and MDM choices.
func (s *Service) ListReferenceData(ctx context.Context, _ *gen.ListReferenceDataPayload) (*gen.OnboardingReferenceData, error) {
	if _, err := s.authorize(ctx, authz.ScopeOrgRead); err != nil {
		return nil, err
	}
	rows, err := loadReference(ctx, repo.New(s.db))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding reference data").LogError(ctx, s.logger)
	}

	plansByProvider := make(map[uuid.UUID][]repo.ListOnboardingPlansRow)
	for _, plan := range rows.plans {
		plansByProvider[plan.ProviderID] = append(plansByProvider[plan.ProviderID], plan)
	}
	productsByProvider := make(map[uuid.UUID][]repo.ListOnboardingProductsRow)
	for _, product := range rows.products {
		productsByProvider[product.ProviderID] = append(productsByProvider[product.ProviderID], product)
	}

	result := &gen.OnboardingReferenceData{
		Providers:  make([]*gen.OnboardingProvider, 0, len(rows.providers)),
		UseCases:   make([]*gen.OnboardingOption, 0, len(UseCases)),
		MdmVendors: make([]*gen.OnboardingOption, 0, len(MDMVendors)),
	}
	for _, provider := range rows.providers {
		result.Providers = append(result.Providers, mv.BuildOnboardingProviderView(provider, plansByProvider[provider.ID], productsByProvider[provider.ID]))
	}
	for _, u := range UseCases {
		result.UseCases = append(result.UseCases, &gen.OnboardingOption{Slug: string(u), Name: u.Name()})
	}
	for _, m := range MDMVendors {
		result.MdmVendors = append(result.MdmVendors, &gen.OnboardingOption{Slug: string(m), Name: m.Name()})
	}
	return result, nil
}

// GetOnboarding returns the organization's answers, stage, planned steps and
// next step.
func (s *Service) GetOnboarding(ctx context.Context, _ *gen.GetOnboardingPayload) (*gen.OnboardingState, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	state, err := s.loadState(ctx, repo.New(s.db), authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding state").LogError(ctx, s.logger)
	}
	return s.stateView(state), nil
}

// SaveStack stores the organization's stack: providers with their plans,
// products and MDM vendor. A previously picked use case is kept and its plan
// recomputed; verified steps are kept because their evidence still holds.
func (s *Service) SaveStack(ctx context.Context, payload *gen.SaveStackPayload) (*gen.OnboardingState, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "saving onboarding answers requires a user identity")
	}
	mdm := MDMVendor(payload.MdmVendor)
	if !slices.Contains(MDMVendors, mdm) {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown MDM vendor %q", payload.MdmVendor)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin onboarding stack save").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	organization, err := queries.GetOrganizationForOnboarding(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load organization for onboarding").LogError(ctx, s.logger)
	}
	before, err := s.loadState(ctx, queries, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding state").LogError(ctx, s.logger)
	}
	reference, err := loadReference(ctx, queries)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding reference data").LogError(ctx, s.logger)
	}
	providerRows, productRows, err := resolveStack(authCtx.ActiveOrganizationID, payload, reference)
	if err != nil {
		return nil, err
	}

	if _, err := queries.UpsertOnboardingStack(ctx, repo.UpsertOnboardingStackParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		MdmVendor:      conv.ToPGText(string(mdm)),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save onboarding stack").LogError(ctx, s.logger)
	}
	if err := queries.DeleteOnboardingSelectedProviders(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reset onboarding providers").LogError(ctx, s.logger)
	}
	for _, row := range providerRows {
		if err := queries.InsertOnboardingSelectedProvider(ctx, row); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save onboarding provider").LogError(ctx, s.logger)
		}
	}
	if err := queries.DeleteOnboardingSelectedProducts(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reset onboarding products").LogError(ctx, s.logger)
	}
	for _, row := range productRows {
		if err := queries.InsertOnboardingSelectedProduct(ctx, row); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save onboarding product").LogError(ctx, s.logger)
		}
	}

	after, err := s.loadState(ctx, queries, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload onboarding state").LogError(ctx, s.logger)
	}
	if err := s.audit.LogOrganizationOnboardingAnswersUpdated(ctx, dbtx, audit.LogOrganizationOnboardingAnswersUpdatedEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		OrganizationName: organization.Name,
		OrganizationSlug: organization.Slug,

		AnswersSnapshotBefore: before.answersView(),
		AnswersSnapshotAfter:  after.answersView(),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit onboarding answers").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit onboarding stack").LogError(ctx, s.logger)
	}
	return s.stateView(after), nil
}

// SelectUseCase picks the one use case to reach first. It needs a saved
// stack. Picking a different use case restarts completion; verified steps
// are kept because their evidence still holds.
func (s *Service) SelectUseCase(ctx context.Context, payload *gen.SelectUseCasePayload) (*gen.OnboardingState, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "saving onboarding answers requires a user identity")
	}
	useCase := UseCase(payload.UseCase)
	if !slices.Contains(UseCases, useCase) {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown use case %q", payload.UseCase)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin onboarding use case save").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	organization, err := queries.GetOrganizationForOnboarding(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load organization for onboarding").LogError(ctx, s.logger)
	}
	before, err := s.loadState(ctx, queries, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding state").LogError(ctx, s.logger)
	}
	if before.answers == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "save the organization's stack before picking a use case")
	}

	if _, err := queries.SetOnboardingUseCase(ctx, repo.SetOnboardingUseCaseParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UseCase:        conv.ToPGText(string(useCase)),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save onboarding use case").LogError(ctx, s.logger)
	}
	if before.answers.UseCase.String != string(useCase) {
		if err := queries.ClearOnboardingCompletion(ctx, authCtx.ActiveOrganizationID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "reset onboarding completion").LogError(ctx, s.logger)
		}
	}

	after, err := s.loadState(ctx, queries, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload onboarding state").LogError(ctx, s.logger)
	}
	if err := s.audit.LogOrganizationOnboardingAnswersUpdated(ctx, dbtx, audit.LogOrganizationOnboardingAnswersUpdatedEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		OrganizationName: organization.Name,
		OrganizationSlug: organization.Slug,

		AnswersSnapshotBefore: before.answersView(),
		AnswersSnapshotAfter:  after.answersView(),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit onboarding answers").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit onboarding use case").LogError(ctx, s.logger)
	}
	return s.stateView(after), nil
}

// VerifyStep checks one planned step against the evidence window. A pass
// records the step, and if the use case is now covered, completes onboarding.
func (s *Service) VerifyStep(ctx context.Context, payload *gen.VerifyStepPayload) (*gen.OnboardingVerifyStepResult, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	state, err := s.loadState(ctx, repo.New(s.db), authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding state").LogError(ctx, s.logger)
	}
	answers, ok := state.plan()
	if !ok {
		return nil, oops.E(oops.CodeBadRequest, nil, "save the stack and pick a use case before verifying a step")
	}
	planned := StepsFor(s.catalog, answers)
	index := slices.IndexFunc(planned, func(step Step) bool { return step.Slug == payload.StepSlug })
	if index < 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "step %q is not part of the organization's plan", payload.StepSlug)
	}
	step := planned[index]

	scope, err := s.evidenceScope(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "scope onboarding evidence").LogError(ctx, s.logger)
	}
	verified, evidence, err := s.checkStep(ctx, scope, step)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check onboarding step evidence").LogError(ctx, s.logger)
	}
	covered := false
	if verified {
		covered, err = s.checkUseCase(ctx, scope, answers.UseCase, selectedSources(s.catalog, answers, func(ProductSpec) bool { return true }))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check onboarding use case evidence").LogError(ctx, s.logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin onboarding step update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	if _, err := queries.UpsertOnboardingStep(ctx, repo.UpsertOnboardingStepParams{OrganizationID: authCtx.ActiveOrganizationID, StepSlug: step.Slug}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record onboarding step").LogError(ctx, s.logger)
	}
	if verified {
		if _, err := queries.MarkOnboardingStepVerified(ctx, repo.MarkOnboardingStepVerifiedParams{OrganizationID: authCtx.ActiveOrganizationID, StepSlug: step.Slug}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mark onboarding step verified").LogError(ctx, s.logger)
		}
		if covered {
			if err := queries.MarkOnboardingCompleted(ctx, authCtx.ActiveOrganizationID); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "mark onboarding completed").LogError(ctx, s.logger)
			}
		}
		organization, err := queries.GetOrganizationForOnboarding(ctx, authCtx.ActiveOrganizationID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load organization for onboarding").LogError(ctx, s.logger)
		}
		if err := s.audit.LogOrganizationOnboardingStepVerified(ctx, dbtx, audit.LogOrganizationOnboardingStepVerifiedEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			OrganizationName: organization.Name,
			OrganizationSlug: organization.Slug,

			StepSlug:  step.Slug,
			UseCase:   string(answers.UseCase),
			Evidence:  evidence,
			Completed: covered,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit onboarding step").LogError(ctx, s.logger)
		}
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit onboarding step").LogError(ctx, s.logger)
	}

	after, err := s.loadState(ctx, repo.New(s.db), authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload onboarding state").LogError(ctx, s.logger)
	}
	return &gen.OnboardingVerifyStepResult{Verified: verified, Evidence: evidence, State: s.stateView(after)}, nil
}

// GetUseCaseStatus reports evidence for every use case, so product pages can
// tell whether their surface is set up regardless of what the admin picked.
func (s *Service) GetUseCaseStatus(ctx context.Context, _ *gen.GetUseCaseStatusPayload) (*gen.OnboardingUseCaseStatusResult, error) {
	authCtx, err := s.authorize(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	state, err := s.loadState(ctx, repo.New(s.db), authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding state").LogError(ctx, s.logger)
	}
	scope, err := s.evidenceScope(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "scope onboarding evidence").LogError(ctx, s.logger)
	}

	answers, planned := state.plan()
	var sources []string
	if state.answers != nil {
		sources = selectedSources(s.catalog, state.stack(), func(ProductSpec) bool { return true })
	}

	result := &gen.OnboardingUseCaseStatusResult{Statuses: make([]*gen.OnboardingUseCaseStatus, 0, len(UseCases))}
	for _, useCase := range UseCases {
		verified, err := s.checkUseCase(ctx, scope, useCase, sources)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check onboarding use case evidence").LogError(ctx, s.logger)
		}
		status := &gen.OnboardingUseCaseStatus{UseCase: string(useCase), Verified: verified, Selected: planned && answers.UseCase == useCase, NextStep: nil}
		if status.Selected && !verified {
			if next, ok := NextStep(s.catalog, answers, state.verified()); ok {
				status.NextStep = s.stepView(next, state.verifiedAt(next.Slug))
			}
		}
		result.Statuses = append(result.Statuses, status)
	}
	return result, nil
}

// orgState is everything stored for one organization's onboarding.
type orgState struct {
	answers   *repo.OrganizationOnboardingAnswer
	providers []repo.ListOnboardingSelectedProvidersRow
	products  []repo.ListOnboardingSelectedProductsRow
	steps     []repo.OrganizationOnboardingStep
}

func (s *Service) loadState(ctx context.Context, queries *repo.Queries, organizationID string) (orgState, error) {
	var state orgState
	answers, err := queries.GetOnboardingAnswers(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return state, nil
	case err != nil:
		return state, fmt.Errorf("get onboarding answers: %w", err)
	}
	state.answers = &answers
	if state.providers, err = queries.ListOnboardingSelectedProviders(ctx, organizationID); err != nil {
		return state, fmt.Errorf("list onboarding providers: %w", err)
	}
	if state.products, err = queries.ListOnboardingSelectedProducts(ctx, organizationID); err != nil {
		return state, fmt.Errorf("list onboarding products: %w", err)
	}
	if state.steps, err = queries.ListOnboardingSteps(ctx, organizationID); err != nil {
		return state, fmt.Errorf("list onboarding steps: %w", err)
	}
	return state, nil
}

// stack converts the stored stack into the rules' input, without a use case.
func (st orgState) stack() Answers {
	answers := Answers{
		UseCase:   "",
		MDM:       MDMVendor(conv.FromPGTextOrEmpty[string](st.answers.MdmVendor)),
		Providers: make([]SelectedProvider, 0, len(st.providers)),
		Products:  make([]string, 0, len(st.products)),
	}
	for _, sel := range st.providers {
		answers.Providers = append(answers.Providers, SelectedProvider{Provider: sel.ProviderSlug, Plan: conv.FromPGTextOrEmpty[string](sel.PlanSlug)})
	}
	for _, sel := range st.products {
		answers.Products = append(answers.Products, sel.ProductSlug)
	}
	return answers
}

// plan converts the stored answers into the rules' input. ok is false until
// the admin has saved the stack and picked a use case.
func (st orgState) plan() (Answers, bool) {
	if st.answers == nil || !st.answers.UseCase.Valid {
		var none Answers
		return none, false
	}
	answers := st.stack()
	answers.UseCase = UseCase(st.answers.UseCase.String)
	return answers, true
}

func (st orgState) stage() Stage {
	switch {
	case st.answers == nil:
		return StageStack
	case !st.answers.UseCase.Valid:
		return StageUseCase
	case st.answers.CompletedAt.Valid:
		return StageDone
	default:
		return StageSteps
	}
}

func (st orgState) verified() map[string]bool {
	verified := make(map[string]bool, len(st.steps))
	for _, step := range st.steps {
		if step.VerifiedAt.Valid {
			verified[step.StepSlug] = true
		}
	}
	return verified
}

func (st orgState) verifiedAt(slug string) *time.Time {
	for _, step := range st.steps {
		if step.StepSlug == slug && step.VerifiedAt.Valid {
			at := step.VerifiedAt.Time
			return &at
		}
	}
	return nil
}

func (st orgState) answersView() *gen.OnboardingAnswers {
	if st.answers == nil {
		return nil
	}
	return mv.BuildOnboardingAnswersView(*st.answers, st.providers, st.products)
}

func (s *Service) stateView(st orgState) *gen.OnboardingState {
	view := &gen.OnboardingState{Stage: string(st.stage()), Answers: st.answersView(), NextStep: nil, Steps: []*gen.OnboardingStep{}, Done: false}
	answers, ok := st.plan()
	if !ok {
		return view
	}
	for _, step := range StepsFor(s.catalog, answers) {
		view.Steps = append(view.Steps, s.stepView(step, st.verifiedAt(step.Slug)))
	}
	view.Done = st.answers.CompletedAt.Valid
	if view.Done {
		return view
	}
	if next, found := NextStep(s.catalog, answers, st.verified()); found {
		view.NextStep = s.stepView(next, nil)
	}
	return view
}

func (s *Service) stepView(step Step, verifiedAt *time.Time) *gen.OnboardingStep {
	view := &gen.OnboardingStep{
		Slug:          step.Slug,
		Title:         step.Title,
		Description:   step.Description,
		TechniqueSlug: step.Technique,
		ProductSlug:   conv.PtrEmpty(step.ProductSlug),
		Destination:   string(step.Destination),
		Evidence:      step.EvidenceText,
		VerifiedAt:    nil,
	}
	if verifiedAt != nil {
		view.VerifiedAt = conv.PtrEmpty(verifiedAt.UTC().Format(time.RFC3339))
	}
	return view
}

// resolveStack checks the selected providers, plans and products against the
// reference tables and returns the rows to store. Every product must belong
// to a selected provider; a provider with plans must have one declared.
func resolveStack(organizationID string, payload *gen.SaveStackPayload, reference referenceRows) ([]repo.InsertOnboardingSelectedProviderParams, []repo.InsertOnboardingSelectedProductParams, error) {
	providersBySlug := make(map[string]repo.OnboardingProvider, len(reference.providers))
	for _, p := range reference.providers {
		providersBySlug[p.Slug] = p
	}
	plansBySlug := make(map[string]repo.ListOnboardingPlansRow, len(reference.plans))
	plansByProvider := make(map[uuid.UUID][]string)
	for _, p := range reference.plans {
		plansBySlug[p.Slug] = p
		plansByProvider[p.ProviderID] = append(plansByProvider[p.ProviderID], p.Slug)
	}
	productsBySlug := make(map[string]repo.ListOnboardingProductsRow, len(reference.products))
	for _, p := range reference.products {
		productsBySlug[p.Slug] = p
	}

	selectedProviders := make(map[uuid.UUID]struct{}, len(payload.Providers))
	providerRows := make([]repo.InsertOnboardingSelectedProviderParams, 0, len(payload.Providers))
	for _, sel := range payload.Providers {
		if sel == nil {
			continue
		}
		provider, ok := providersBySlug[sel.ProviderSlug]
		if !ok {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "unknown provider %q", sel.ProviderSlug)
		}
		if _, dup := selectedProviders[provider.ID]; dup {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "provider %q is selected twice", sel.ProviderSlug)
		}
		selectedProviders[provider.ID] = struct{}{}

		row := repo.InsertOnboardingSelectedProviderParams{OrganizationID: organizationID, ProviderID: provider.ID, PlanID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
		planSlug := conv.PtrValOr(sel.PlanSlug, "")
		allowed := plansByProvider[provider.ID]
		switch {
		case len(allowed) == 0 && planSlug != "":
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "provider %q has no plans", sel.ProviderSlug)
		case len(allowed) > 0 && planSlug == "":
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "provider %q needs a plan", sel.ProviderSlug)
		case planSlug != "":
			if !slices.Contains(allowed, planSlug) {
				return nil, nil, oops.E(oops.CodeBadRequest, nil, "plan %q does not belong to provider %q", planSlug, sel.ProviderSlug)
			}
			row.PlanID = uuid.NullUUID{UUID: plansBySlug[planSlug].ID, Valid: true}
		}
		providerRows = append(providerRows, row)
	}
	if len(providerRows) == 0 {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "select at least one provider")
	}

	seenProducts := make(map[string]struct{}, len(payload.ProductSlugs))
	productRows := make([]repo.InsertOnboardingSelectedProductParams, 0, len(payload.ProductSlugs))
	for _, slug := range payload.ProductSlugs {
		product, ok := productsBySlug[slug]
		if !ok {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "unknown product %q", slug)
		}
		if _, dup := seenProducts[slug]; dup {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "product %q is selected twice", slug)
		}
		seenProducts[slug] = struct{}{}
		if _, ok := selectedProviders[product.ProviderID]; !ok {
			return nil, nil, oops.E(oops.CodeBadRequest, nil, "product %q belongs to a provider that is not selected", slug)
		}
		productRows = append(productRows, repo.InsertOnboardingSelectedProductParams{OrganizationID: organizationID, ProductID: product.ID})
	}
	if len(productRows) == 0 {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "select at least one product")
	}
	return providerRows, productRows, nil
}
