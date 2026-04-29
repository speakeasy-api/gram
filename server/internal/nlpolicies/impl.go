package nlpolicies

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/nlpolicies/server"
	gen "github.com/speakeasy-api/gram/server/gen/nlpolicies"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

// Service is the stub implementation used in PR 1. All handlers return data
// from fixtures.go. Replaced by DB-backed implementation in PR 3.
type Service struct {
	logger *slog.Logger
}

func NewService(logger *slog.Logger) *Service {
	return &Service{logger: logger.With(attr.SlogComponent("nlpolicies"))}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

// APIKeyAuth — stubbed; real impl in PR 3 uses sessions.Manager.
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	s.logger.WarnContext(ctx, "nlpolicies APIKeyAuth stub: accepting any key — DO NOT MERGE PR 3 without replacing")
	_ = key
	_ = schema
	return ctx, nil
}

// Handlers — every method returns fixture data ignoring tenant.

func (s *Service) CreatePolicy(_ context.Context, p *gen.CreatePolicyPayload) (*types.NLPolicy, error) {
	pol := fixturePolicies()[0]
	pol.Name = p.Name
	if p.Description != nil {
		pol.Description = p.Description
	}
	pol.NlPrompt = p.NlPrompt
	if p.ScopePerCall != nil {
		pol.ScopePerCall = *p.ScopePerCall
	}
	if p.ScopeSession != nil {
		pol.ScopeSession = *p.ScopeSession
	}
	if p.FailMode != nil {
		pol.FailMode = *p.FailMode
	}
	if p.StaticRules != nil {
		pol.StaticRules = *p.StaticRules
	}
	return pol, nil
}

func (s *Service) ListPolicies(_ context.Context, _ *gen.ListPoliciesPayload) (*gen.ListPoliciesResult, error) {
	return &gen.ListPoliciesResult{Policies: fixturePolicies()}, nil
}

func (s *Service) GetPolicy(ctx context.Context, p *gen.GetPolicyPayload) (*types.NLPolicy, error) {
	for _, pol := range fixturePolicies() {
		if pol.ID == p.PolicyID {
			return pol, nil
		}
	}
	return nil, oops.E(oops.CodeNotFound, nil, "policy not found").Log(ctx, s.logger)
}

func (s *Service) UpdatePolicy(ctx context.Context, p *gen.UpdatePolicyPayload) (*types.NLPolicy, error) {
	pol, err := s.GetPolicy(ctx, &gen.GetPolicyPayload{PolicyID: p.PolicyID})
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		pol.Name = *p.Name
	}
	if p.Description != nil {
		pol.Description = p.Description
	}
	if p.NlPrompt != nil {
		pol.NlPrompt = *p.NlPrompt
	}
	if p.ScopePerCall != nil {
		pol.ScopePerCall = *p.ScopePerCall
	}
	if p.ScopeSession != nil {
		pol.ScopeSession = *p.ScopeSession
	}
	if p.FailMode != nil {
		pol.FailMode = *p.FailMode
	}
	if p.StaticRules != nil {
		pol.StaticRules = *p.StaticRules
	}
	pol.Version++
	return pol, nil
}

func (s *Service) SetMode(ctx context.Context, p *gen.SetModePayload) (*types.NLPolicy, error) {
	pol, err := s.GetPolicy(ctx, &gen.GetPolicyPayload{PolicyID: p.PolicyID})
	if err != nil {
		return nil, err
	}
	pol.Mode = p.Mode
	return pol, nil
}

func (s *Service) DeletePolicy(_ context.Context, _ *gen.DeletePolicyPayload) error {
	return nil
}

func (s *Service) ListDecisions(_ context.Context, p *gen.ListDecisionsPayload) (*gen.ListDecisionsResult, error) {
	all := fixtureDecisions()
	out := make([]*types.NLPolicyDecision, 0, len(all))
	for _, d := range all {
		if d.NlPolicyID == p.PolicyID {
			out = append(out, d)
		}
	}
	return &gen.ListDecisionsResult{Decisions: out}, nil
}

func (s *Service) ListSessionVerdicts(_ context.Context, p *gen.ListSessionVerdictsPayload) (*gen.ListSessionVerdictsResult, error) {
	all := fixtureSessionVerdicts()
	out := make([]*types.NLPolicySessionVerdict, 0, len(all))
	for _, v := range all {
		if v.NlPolicyID == p.PolicyID {
			out = append(out, v)
		}
	}
	return &gen.ListSessionVerdictsResult{Verdicts: out}, nil
}

func (s *Service) ClearSessionVerdict(ctx context.Context, p *gen.ClearSessionVerdictPayload) (*types.NLPolicySessionVerdict, error) {
	for _, v := range fixtureSessionVerdicts() {
		if v.ID == p.VerdictID {
			now := "2026-04-28T12:00:00Z"
			v.ClearedAt = &now
			return v, nil
		}
	}
	return nil, oops.E(oops.CodeNotFound, nil, "session verdict not found").Log(ctx, s.logger)
}

func (s *Service) Replay(_ context.Context, _ *gen.ReplayPayload) (*types.NLPolicyReplayRun, error) {
	return fixtureReplayRun(), nil
}

func (s *Service) GetReplayRun(ctx context.Context, p *gen.GetReplayRunPayload) (*types.NLPolicyReplayRun, error) {
	run := fixtureReplayRun()
	if p.RunID != run.ID {
		return nil, oops.E(oops.CodeNotFound, nil, "replay run not found").Log(ctx, s.logger)
	}
	return run, nil
}

func (s *Service) ListReplayResults(_ context.Context, _ *gen.ListReplayResultsPayload) (*gen.ListReplayResultsResult, error) {
	// Synthesize 100 results matching the canned counts: 14 BLOCK, 84 ALLOW, 2 JUDGE_ERROR.
	results := make([]*types.NLPolicyReplayResult, 0, 100)
	now := "2026-04-28T11:55:00Z"
	for i := range 100 {
		decision := "ALLOW"
		switch {
		case i < 14:
			decision = "BLOCK"
		case i < 16:
			decision = "JUDGE_ERROR"
		}
		results = append(results, &types.NLPolicyReplayResult{
			ID:          uuid.New().String(),
			ReplayRunID: "00000000-0000-4000-a000-00000000a8f2",
			Decision:    decision,
			CreatedAt:   now,
		})
	}
	return &gen.ListReplayResultsResult{Results: results}, nil
}
