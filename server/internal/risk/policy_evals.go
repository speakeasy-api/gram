package risk

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// policyEvalRetention bounds how long an eval run and its findings (which carry
// raw match text) live before the GC sweep deletes them.
const policyEvalRetention = 30 * 24 * time.Hour

// PolicyEvalRunner starts and cancels the Temporal workflow that executes a
// policy eval ("session replay") run. Optional: when nil (e.g. the lightweight
// observer) run creation/cancellation skips workflow orchestration.
type PolicyEvalRunner interface {
	Start(ctx context.Context, runID, projectID uuid.UUID) error
	Cancel(ctx context.Context, runID, projectID uuid.UUID) error
}

// CreatePolicyEvalRun starts a non-enforcing eval of either a saved policy or an
// unsaved candidate config over a sample of historical messages.
func (s *Service) CreatePolicyEvalRun(ctx context.Context, payload *gen.CreatePolicyEvalRunPayload) (*gen.PolicyEvalRun, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if payload.Sample == nil {
		return nil, oops.E(oops.CodeInvalid, nil, "sample is required")
	}
	hasPolicy := payload.PolicyID != nil && *payload.PolicyID != ""
	hasCandidate := payload.Candidate != nil
	if hasPolicy == hasCandidate {
		return nil, oops.E(oops.CodeInvalid, nil, "provide exactly one of policy_id or candidate")
	}

	spec, err := evalSampleSpecFromPayload(payload.Sample)
	if err != nil {
		return nil, err
	}
	sampleJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode sample definition").LogError(ctx, s.logger)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate eval run id").LogError(ctx, s.logger)
	}

	params := repo.CreatePolicyEvalRunParams{
		ID:                id,
		ProjectID:         *authCtx.ProjectID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		RiskPolicyID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RiskPolicyVersion: pgtype.Int8{Int64: 0, Valid: false},
		ConfigSnapshot:    nil,
		SampleDefinition:  sampleJSON,
		RequestedBy:       pgtype.Text{String: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String(), Valid: authCtx.UserID != ""},
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(policyEvalRetention), InfinityModifier: pgtype.Finite, Valid: true},
	}

	if hasPolicy {
		policyID, err := uuid.Parse(*payload.PolicyID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid policy_id")
		}
		policy, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID, ProjectID: *authCtx.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "risk policy not found")
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "get risk policy").LogError(ctx, s.logger)
		}
		params.RiskPolicyID = uuid.NullUUID{UUID: policy.ID, Valid: true}
		params.RiskPolicyVersion = pgtype.Int8{Int64: policy.Version, Valid: true}
	} else {
		snap, err := s.evalConfigSnapshotFromCandidate(ctx, authCtx, payload.Candidate)
		if err != nil {
			return nil, err
		}
		snapJSON, err := json.Marshal(snap)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "encode candidate config").LogError(ctx, s.logger)
		}
		params.ConfigSnapshot = snapJSON
	}

	row, err := s.repo.CreatePolicyEvalRun(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create policy eval run").LogError(ctx, s.logger)
	}

	if s.evalRunner != nil {
		if err := s.evalRunner.Start(ctx, row.ID, row.ProjectID); err != nil {
			// Mark the run failed so it doesn't linger 'pending' with no worker.
			_ = s.repo.FailPolicyEvalRun(ctx, repo.FailPolicyEvalRunParams{
				Error:     pgtype.Text{String: "failed to start eval workflow", Valid: true},
				ID:        row.ID,
				ProjectID: row.ProjectID,
			})
			return nil, oops.E(oops.CodeUnexpected, err, "start policy eval run").LogError(ctx, s.logger)
		}
	}

	return evalRunRowToType(row), nil
}

// ListPolicyEvalRuns lists a project's eval runs newest-first, optionally
// filtered to a single policy.
func (s *Service) ListPolicyEvalRuns(ctx context.Context, payload *gen.ListPolicyEvalRunsPayload) (*gen.ListPolicyEvalRunsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").LogError(ctx, s.logger)
	}
	pageSize := resolvePageSize(payload.Limit)

	var policyFilter uuid.NullUUID
	if payload.PolicyID != nil && *payload.PolicyID != "" {
		pid, err := uuid.Parse(*payload.PolicyID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid policy_id")
		}
		policyFilter = uuid.NullUUID{UUID: pid, Valid: true}
	}

	cursorTime, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListPolicyEvalRuns(ctx, repo.ListPolicyEvalRunsParams{
		ProjectID:       *authCtx.ProjectID,
		RiskPolicyID:    policyFilter,
		CursorCreatedAt: cursorTime,
		CursorID:        cursorID,
		ResultLimit:     conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list policy eval runs").LogError(ctx, s.logger)
	}

	var nextCursor *string
	if len(rows) > pageSize {
		last := rows[pageSize-1]
		encoded := encodeRiskResultsCursor(riskResultsCursor{MessageCreatedAt: last.CreatedAt.Time, ID: last.ID})
		nextCursor = &encoded
		rows = rows[:pageSize]
	}

	runs := make([]*gen.PolicyEvalRun, 0, len(rows))
	for _, r := range rows {
		runs = append(runs, evalRunRowToType(r))
	}
	return &gen.ListPolicyEvalRunsResult{Runs: runs, NextCursor: nextCursor}, nil
}

// GetPolicyEvalRun returns a single run (with its rolled-up stats).
func (s *Service) GetPolicyEvalRun(ctx context.Context, payload *gen.GetPolicyEvalRunPayload) (*gen.PolicyEvalRun, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid eval run id")
	}
	row, err := s.repo.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: id, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "policy eval run not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get policy eval run").LogError(ctx, s.logger)
	}
	return evalRunRowToType(row), nil
}

// ListPolicyEvalFindings lists a run's findings with sample message context.
func (s *Service) ListPolicyEvalFindings(ctx context.Context, payload *gen.ListPolicyEvalFindingsPayload) (*gen.ListPolicyEvalFindingsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	runID, err := uuid.Parse(payload.RunID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid run_id")
	}
	// Scope the run to this project (clean 404 + cross-project read protection).
	if _, err := s.repo.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: runID, ProjectID: *authCtx.ProjectID}); errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "policy eval run not found")
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get policy eval run").LogError(ctx, s.logger)
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor").LogError(ctx, s.logger)
	}
	pageSize := resolvePageSize(payload.Limit)

	cursorTime, cursorID := cursorToParams(cursor)
	rows, err := s.repo.ListPolicyEvalFindings(ctx, repo.ListPolicyEvalFindingsParams{
		ProjectID:       *authCtx.ProjectID,
		PolicyEvalRunID: runID,
		CursorCreatedAt: cursorTime,
		CursorID:        cursorID,
		ResultLimit:     conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list policy eval findings").LogError(ctx, s.logger)
	}

	var nextCursor *string
	if len(rows) > pageSize {
		last := rows[pageSize-1]
		encoded := encodeRiskResultsCursor(riskResultsCursor{MessageCreatedAt: last.CreatedAt.Time, ID: last.ID})
		nextCursor = &encoded
		rows = rows[:pageSize]
	}

	findings := make([]*gen.PolicyEvalFinding, 0, len(rows))
	for _, r := range rows {
		findings = append(findings, evalFindingRowToType(r))
	}
	return &gen.ListPolicyEvalFindingsResult{Findings: findings, NextCursor: nextCursor}, nil
}

// policyEvalInsightsMatchLimit caps the byMatch cluster list: the duplicated
// matches are the actionable ones and the dashboard only surfaces the top few.
const policyEvalInsightsMatchLimit = 20

// GetPolicyEvalRunInsights aggregates ALL of a run's findings (not paginated)
// into actionable clusters: by matched value (deduped, secrets redacted), by
// (source, rule_id), and by message type. Mirrors ListPolicyEvalFindings for
// auth/scoping.
func (s *Service) GetPolicyEvalRunInsights(ctx context.Context, payload *gen.GetPolicyEvalRunInsightsPayload) (*gen.PolicyEvalRunInsights, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	runID, err := uuid.Parse(payload.RunID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid run_id")
	}
	// Scope the run to this project (clean 404 + cross-project read protection).
	if _, err := s.repo.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: runID, ProjectID: *authCtx.ProjectID}); errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "policy eval run not found")
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get policy eval run").LogError(ctx, s.logger)
	}

	matchRows, err := s.repo.AggregatePolicyEvalFindingsByMatch(ctx, repo.AggregatePolicyEvalFindingsByMatchParams{
		ProjectID:       *authCtx.ProjectID,
		PolicyEvalRunID: runID,
		ResultLimit:     policyEvalInsightsMatchLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "aggregate eval findings by match").LogError(ctx, s.logger)
	}

	ruleRows, err := s.repo.AggregatePolicyEvalFindingsByRule(ctx, repo.AggregatePolicyEvalFindingsByRuleParams{
		ProjectID:       *authCtx.ProjectID,
		PolicyEvalRunID: runID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "aggregate eval findings by rule").LogError(ctx, s.logger)
	}

	roleRows, err := s.repo.AggregatePolicyEvalFindingsByMessageType(ctx, repo.AggregatePolicyEvalFindingsByMessageTypeParams{
		ProjectID:       *authCtx.ProjectID,
		PolicyEvalRunID: runID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "aggregate eval findings by message type").LogError(ctx, s.logger)
	}

	byMatch := make([]*gen.PolicyEvalMatchCluster, 0, len(matchRows))
	for _, r := range matchRows {
		byMatch = append(byMatch, evalMatchClusterRowToType(r, authCtx.ActiveOrganizationID))
	}
	byRule := make([]*gen.PolicyEvalRuleCluster, 0, len(ruleRows))
	for _, r := range ruleRows {
		byRule = append(byRule, evalRuleClusterRowToType(r))
	}
	byMessageType := make([]*gen.PolicyEvalMessageTypeCluster, 0, len(roleRows))
	for _, r := range roleRows {
		byMessageType = append(byMessageType, &gen.PolicyEvalMessageTypeCluster{Role: r.Role, Count: r.FindingCount})
	}

	return &gen.PolicyEvalRunInsights{
		ByMatch:       byMatch,
		ByRule:        byRule,
		ByMessageType: byMessageType,
	}, nil
}

// CancelPolicyEvalRun cancels an in-progress run and signals its workflow.
func (s *Service) CancelPolicyEvalRun(ctx context.Context, payload *gen.CancelPolicyEvalRunPayload) (*gen.PolicyEvalRun, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid eval run id")
	}
	if _, err := s.repo.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: id, ProjectID: *authCtx.ProjectID}); errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "policy eval run not found")
	} else if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get policy eval run").LogError(ctx, s.logger)
	}

	if err := s.repo.CancelPolicyEvalRun(ctx, repo.CancelPolicyEvalRunParams{ID: id, ProjectID: *authCtx.ProjectID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cancel policy eval run").LogError(ctx, s.logger)
	}

	// Best-effort: cancel the workflow. The DB status is already 'cancelled', so
	// a failed signal is non-fatal.
	if s.evalRunner != nil {
		if err := s.evalRunner.Cancel(ctx, id, *authCtx.ProjectID); err != nil {
			s.logger.WarnContext(ctx, "cancel policy eval run workflow failed", attr.SlogError(err))
		}
	}

	row, err := s.repo.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: id, ProjectID: *authCtx.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get policy eval run").LogError(ctx, s.logger)
	}
	return evalRunRowToType(row), nil
}

// --- validation / mapping helpers ------------------------------------------

func evalSampleSpecFromPayload(p *gen.PolicyEvalSampleDefinition) (ra.EvalSampleSpec, error) {
	mode := p.Mode
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "manual" {
		return ra.EvalSampleSpec{}, oops.E(oops.CodeInvalid, nil, "invalid sample mode")
	}
	spec := ra.EvalSampleSpec{
		Mode:          mode,
		LookbackDays:  0,
		LastNSessions: 0,
		MaxMessages:   p.MaxMessages,
		MessageIDs:    nil,
	}
	if p.LookbackDays != nil {
		spec.LookbackDays = *p.LookbackDays
	}
	if p.LastNSessions != nil {
		spec.LastNSessions = *p.LastNSessions
	}
	if mode == "manual" {
		if len(p.MessageIds) == 0 {
			return ra.EvalSampleSpec{}, oops.E(oops.CodeInvalid, nil, "manual sample requires message_ids")
		}
		ids := make([]string, 0, len(p.MessageIds))
		for _, raw := range p.MessageIds {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				return ra.EvalSampleSpec{}, oops.E(oops.CodeInvalid, err, "invalid message id in sample")
			}
			ids = append(ids, parsed.String())
		}
		spec.MessageIDs = ids
	}
	return spec, nil
}

func (s *Service) evalConfigSnapshotFromCandidate(ctx context.Context, authCtx *contextvalues.AuthContext, c *gen.PolicyEvalCandidateConfig) (ra.EvalConfigSnapshot, error) {
	policyType := c.PolicyType
	if policyType == "" {
		policyType = ra.PolicyTypeStandard
	}
	if err := validatePolicyType(policyType); err != nil {
		return ra.EvalConfigSnapshot{}, err
	}

	if err := validateScopeExpr(s.celEng, c.ScopeInclude); err != nil {
		return ra.EvalConfigSnapshot{}, oops.E(oops.CodeInvalid, err, "invalid scope_include")
	}
	if err := validateScopeExpr(s.celEng, c.ScopeExempt); err != nil {
		return ra.EvalConfigSnapshot{}, oops.E(oops.CodeInvalid, err, "invalid scope_exempt")
	}
	if err := validateMessageTypes(c.MessageTypes); err != nil {
		return ra.EvalConfigSnapshot{}, err
	}

	snap := ra.EvalConfigSnapshot{
		PolicyType:       policyType,
		Sources:          nil,
		PresidioEntities: nil,
		DisabledRules:    c.DisabledRules,
		CustomRuleIDs:    c.CustomRuleIds,
		MessageTypes:     c.MessageTypes,
		ScopeInclude:     conv.PtrValOr(c.ScopeInclude, ""),
		ScopeExempt:      conv.PtrValOr(c.ScopeExempt, ""),
		Prompt:           "",
		ModelConfig:      nil,
	}

	if policyType == ra.PolicyTypePromptBased {
		if !s.promptPoliciesEnabled(ctx, authCtx) {
			return ra.EvalConfigSnapshot{}, oops.E(oops.CodeForbidden, nil, "prompt-based policies are not enabled for this organization")
		}
		prompt, mc, err := validatePromptPolicyFields(c.Prompt, c.ModelConfig)
		if err != nil {
			return ra.EvalConfigSnapshot{}, err
		}
		snap.Prompt = prompt
		snap.ModelConfig = mc
	} else {
		sources := c.Sources
		if sources == nil {
			sources = []string{ra.SourceGitleaks}
		}
		if err := validateSources(sources); err != nil {
			return ra.EvalConfigSnapshot{}, err
		}
		if err := validateCustomRuleIDs(c.CustomRuleIds); err != nil {
			return ra.EvalConfigSnapshot{}, err
		}
		snap.Sources = sources
		snap.PresidioEntities = c.PresidioEntities
	}
	return snap, nil
}

func evalRunRowToType(row repo.PolicyEvalRun) *gen.PolicyEvalRun {
	inputTokens := row.InputTokens
	outputTokens := row.OutputTokens
	out := &gen.PolicyEvalRun{
		ID:                row.ID.String(),
		RiskPolicyID:      nil,
		RiskPolicyVersion: nil,
		Status:            row.Status,
		Sample:            nil,
		RequestedBy:       conv.FromPGText[string](row.RequestedBy),
		MessagesScanned:   int64(row.MessagesScanned),
		FindingsCount:     int64(row.FindingsCount),
		TotalCostUsd:      row.TotalCostUsd,
		InputTokens:       &inputTokens,
		OutputTokens:      &outputTokens,
		JudgeLatencyP50Ms: nil,
		JudgeLatencyP95Ms: nil,
		Error:             conv.FromPGText[string](row.Error),
		CreatedAt:         row.CreatedAt.Time.Format(time.RFC3339),
		StartedAt:         pgTimePtr(row.StartedAt),
		CompletedAt:       pgTimePtr(row.CompletedAt),
		ExpiresAt:         pgTimePtr(row.ExpiresAt),
	}
	if row.RiskPolicyID.Valid {
		pid := row.RiskPolicyID.UUID.String()
		out.RiskPolicyID = &pid
	}
	if row.RiskPolicyVersion.Valid {
		v := row.RiskPolicyVersion.Int64
		out.RiskPolicyVersion = &v
	}
	if row.JudgeLatencyP50Ms.Valid {
		v := int(row.JudgeLatencyP50Ms.Int32)
		out.JudgeLatencyP50Ms = &v
	}
	if row.JudgeLatencyP95Ms.Valid {
		v := int(row.JudgeLatencyP95Ms.Int32)
		out.JudgeLatencyP95Ms = &v
	}
	var spec ra.EvalSampleSpec
	if err := json.Unmarshal(row.SampleDefinition, &spec); err == nil {
		out.Sample = evalSampleSpecToType(spec)
	}
	return out
}

func evalSampleSpecToType(spec ra.EvalSampleSpec) *gen.PolicyEvalSampleDefinition {
	out := &gen.PolicyEvalSampleDefinition{
		Mode:          spec.Mode,
		LookbackDays:  nil,
		LastNSessions: nil,
		MaxMessages:   spec.MaxMessages,
		MessageIds:    spec.MessageIDs,
	}
	if spec.LookbackDays > 0 {
		v := spec.LookbackDays
		out.LookbackDays = &v
	}
	if spec.LastNSessions > 0 {
		v := spec.LastNSessions
		out.LastNSessions = &v
	}
	return out
}

func evalFindingRowToType(row repo.ListPolicyEvalFindingsRow) *gen.PolicyEvalFinding {
	out := &gen.PolicyEvalFinding{
		ID:            row.ID.String(),
		RunID:         row.PolicyEvalRunID.String(),
		ChatMessageID: row.ChatMessageID.String(),
		Source:        row.Source,
		RuleID:        conv.FromPGText[string](row.RuleID),
		Description:   conv.FromPGText[string](row.Description),
		Match:         conv.FromPGText[string](row.Match),
		StartPos:      conv.PtrInt32ToInt(conv.FromPGInt4(row.StartPos)),
		EndPos:        conv.PtrInt32ToInt(conv.FromPGInt4(row.EndPos)),
		Confidence:    conv.FromPGFloat8(row.Confidence),
		Tags:          row.Tags,
		CreatedAt:     row.CreatedAt.Time.Format(time.RFC3339),
		ChatID:        nil,
		ChatTitle:     conv.FromPGText[string](row.ChatTitle),
		ChatUserID:    conv.FromPGText[string](row.ChatUserID),
	}
	if row.ChatID != uuid.Nil {
		cid := row.ChatID.String()
		out.ChatID = &cid
	}
	return out
}

// evalMatchClusterRowToType maps a byMatch aggregate row to its API type. The
// raw match (MatchSample, a representative pulled solely so the server can mask
// it) is NEVER returned verbatim: it is passed through redactMatch — the same
// helper used for the agent-facing redacted results — so secrets/PII leave the
// server only as an opaque length+sha fingerprint (or, for non-sensitive sources
// like shadow_mcp, the value as the rest of the risk UI shows it).
func evalMatchClusterRowToType(row repo.AggregatePolicyEvalFindingsByMatchRow, orgID string) *gen.PolicyEvalMatchCluster {
	sample := row.MatchSample
	return &gen.PolicyEvalMatchCluster{
		MatchHash:        row.MatchHash,
		MatchRedacted:    redactMatch(row.Source, &sample, orgID),
		Source:           row.Source,
		RuleID:           conv.FromPGText[string](row.RuleID),
		Count:            row.FindingCount,
		DistinctSessions: row.DistinctSessions,
	}
}

func evalRuleClusterRowToType(row repo.AggregatePolicyEvalFindingsByRuleRow) *gen.PolicyEvalRuleCluster {
	return &gen.PolicyEvalRuleCluster{
		Source:           row.Source,
		RuleID:           conv.FromPGText[string](row.RuleID),
		Count:            row.FindingCount,
		DistinctMessages: row.DistinctMessages,
	}
}

func pgTimePtr(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	formatted := ts.Time.Format(time.RFC3339)
	return &formatted
}
