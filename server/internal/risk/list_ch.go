package risk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// MaxExecutionIDLen bounds an execution_id filter. Execution IDs are UUIDs;
// the slack only rejects obviously bogus input.
const MaxExecutionIDLen = 128

// ParseExecutionIDFilter canonicalizes an execution_id filter, "" meaning
// none. A blank or over-long value is invalid rather than dropped, since
// dropping it would list every finding.
func ParseExecutionIDFilter(raw string) (string, bool) {
	id := CanonicalExecutionID(raw)
	if (raw != "" && id == "") || len(id) > MaxExecutionIDLen {
		return "", false
	}
	return id, true
}

// CanonicalExecutionID trims an execution_id filter and lowercases it when it
// is a UUID, the form findings store, since the filter is an exact match.
func CanonicalExecutionID(id string) string {
	id = strings.TrimSpace(id)
	if parsed, err := uuid.Parse(id); err == nil {
		return parsed.String()
	}
	return id
}

// listResultsByProjectFromClickHouse serves the project-wide (non-chat-scoped)
// ListRiskResults page from the ClickHouse risk_findings table, the only store
// holding MCP-seam findings. Rows come back pre-redacted (the store never
// holds raw match content), so Match and Spans are always nil and
// MatchRedacted carries the ingest-time display string.
// Chat titles and tool-call block ids are enriched from Postgres per page
// because both mutate after ingest.
func (s *Service) listResultsByProjectFromClickHouse(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	cursor *riskResultsCursor,
	pageSize int,
	policyID uuid.NullUUID,
	mcpServerID, chatID string,
	resultID uuid.NullUUID,
	executionID string,
	category, ruleID, userID string,
	externalUserIDs []string,
	uniqueMatch, nonAssistant bool,
	assistantID uuid.NullUUID,
	from, to *time.Time,
) (*gen.ListRiskResultsResult, error) {
	projectID := *authCtx.ProjectID

	policyIDs, err := s.visiblePolicyIDs(ctx, projectID, policyID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve visible risk policies").LogError(ctx, s.logger)
	}
	if len(policyIDs) == 0 {
		return &gen.ListRiskResultsResult{Results: []*types.RiskResult{}, TotalCount: 0, NextCursor: nil}, nil
	}

	params := chrepo.ListRiskFindingsParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       projectID.String(),
		MCPServerID:     mcpServerID,
		ChatID:          chatID,
		ResultID:        resultID,
		ExecutionID:     executionID,
		PolicyIDs:       policyIDs,
		From:            from,
		To:              to,
		Category:        category,
		RuleIDSubstr:    ruleID,
		UserIDSubstr:    userID,
		ExternalUserIDs: externalUserIDs,
		AssistantID:     "",
		NonAssistant:    nonAssistant,
		UniqueMatch:     uniqueMatch,
		CursorTime:      nil,
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		// resolvePageSize bounds pageSize to [1, 200], so the conversion
		// cannot wrap.
		Limit: uint64(conv.SafeInt32(pageSize)) + 1, // #nosec G115 -- non-negative by construction.
	}
	if assistantID.Valid {
		params.AssistantID = assistantID.UUID.String()
	}
	if cursor != nil {
		cursorTime := cursor.MessageCreatedAt
		params.CursorTime = &cursorTime
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}

	rows, err := s.findingsCH.ListRiskFindings(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk findings from clickhouse").LogError(ctx, s.logger)
	}

	// Best-effort total: a failed count degrades to zero rather than failing
	// the page.
	var totalCount int64
	if count, err := s.findingsCH.CountRiskFindings(ctx, params); err != nil {
		s.logger.WarnContext(ctx, "count risk findings from clickhouse", attr.SlogError(err))
	} else {
		totalCount = safeCount(count)
	}

	titles, blocks, userEmails := s.listDisplayEnrichment(ctx, authCtx.ActiveOrganizationID, projectID, rows)

	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		results = append(results, chListRowToResult(row, titles, blocks, userEmails))
		if i == pageSize {
			// Cursor from the LAST RETURNED row (not this extra row): the
			// next-page predicate is a strict (message_created_at, id) <, so a
			// cursor pointing at the extra row would skip it entirely.
			last := rows[pageSize-1]
			nextCursor = &riskResultsCursor{MessageCreatedAt: last.MessageCreatedAt, ID: last.ID}
		}
	}
	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

// visiblePolicyIDs resolves the policy pushdown: ClickHouse cannot join
// risk_policies, so the Postgres side decides which policies the listing may
// show. The default view covers every non-deleted policy, disabled ones
// included, so disabling a policy keeps its history visible. An explicit
// policy filter narrows to that policy. Deleted policies resolve to nothing;
// their ClickHouse rows linger until TTL and the pushdown is what hides them.
func (s *Service) visiblePolicyIDs(ctx context.Context, projectID uuid.UUID, policyID uuid.NullUUID) ([]string, error) {
	if policyID.Valid {
		if _, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID.UUID, ProjectID: projectID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, fmt.Errorf("get risk policy: %w", err)
		}
		return []string{policyID.UUID.String()}, nil
	}

	policies, err := s.repo.ListRiskPolicies(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list risk policies: %w", err)
	}
	ids := make([]string, 0, len(policies))
	for _, policy := range policies {
		ids = append(ids, policy.ID.String())
	}
	return ids, nil
}

// listDisplayEnrichment batch-fetches the page's chat titles (by chat id),
// latest tool-call block ids (by chat message id), and MCP users' current
// emails from Postgres. Best-effort: a failed lookup logs and leaves the
// affected display fields on their existing fallbacks rather than failing the
// page.
func (s *Service) listDisplayEnrichment(ctx context.Context, organizationID string, projectID uuid.UUID, rows []chrepo.RiskFindingListRow) (map[uuid.UUID]string, map[uuid.UUID]uuid.UUID, map[string]string) {
	chatIDs := make([]uuid.UUID, 0, len(rows))
	messageIDs := make([]uuid.UUID, 0, len(rows))
	seenChats := make(map[uuid.UUID]struct{}, len(rows))
	seenMessages := make(map[uuid.UUID]struct{}, len(rows))
	userIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if id, err := uuid.Parse(row.ChatID); err == nil {
			if _, ok := seenChats[id]; !ok {
				seenChats[id] = struct{}{}
				chatIDs = append(chatIDs, id)
			}
		}
		if id, err := uuid.Parse(row.ChatMessageID); err == nil {
			if _, ok := seenMessages[id]; !ok {
				seenMessages[id] = struct{}{}
				messageIDs = append(messageIDs, id)
			}
		}
		if isMCPFindingRow(row) && row.UserID != "" {
			userIDs = append(userIDs, row.UserID)
		}
	}

	titles := make(map[uuid.UUID]string, len(chatIDs))
	if len(chatIDs) > 0 {
		rows, err := s.repo.ListChatTitlesByIDs(ctx, repo.ListChatTitlesByIDsParams{ProjectID: projectID, Ids: chatIDs})
		if err != nil {
			s.logger.WarnContext(ctx, "enrich risk listing chat titles", attr.SlogError(err))
		}
		for _, row := range rows {
			if title := conv.FromPGText[string](row.Title); title != nil {
				titles[row.ID] = *title
			}
		}
	}

	blocks := make(map[uuid.UUID]uuid.UUID, len(messageIDs))
	if len(messageIDs) > 0 {
		rows, err := s.repo.ListLatestToolCallBlocksByMessageIDs(ctx, repo.ListLatestToolCallBlocksByMessageIDsParams{ProjectID: projectID, Ids: messageIDs})
		if err != nil {
			s.logger.WarnContext(ctx, "enrich risk listing tool call blocks", attr.SlogError(err))
		}
		for _, row := range rows {
			if row.ChatMessageID.Valid {
				blocks[row.ChatMessageID.UUID] = row.BlockID
			}
		}
	}

	userEmails, err := s.riskUserEmailsByID(ctx, organizationID, userIDs)
	if err != nil {
		s.logger.WarnContext(ctx, "enrich risk listing user emails", attr.SlogError(err))
		userEmails = map[string]string{}
	}

	return titles, blocks, userEmails
}

func isMCPFindingRow(row chrepo.RiskFindingListRow) bool {
	return row.MCPServerID != "" ||
		row.MetaMCPServerID != "" ||
		row.ToolsetID != "" ||
		row.MediationSurface != "" ||
		row.MCPMethod != ""
}

// chListRowToResult maps one ClickHouse listing row to the API type. The
// store-side redaction is authoritative: Match and Spans stay nil and
// MatchRedacted carries the ingest-time display string, so the public
// ListRiskResults redaction pass recognizes the row as already redacted.
func chListRowToResult(row chrepo.RiskFindingListRow, titles map[uuid.UUID]string, blocks map[uuid.UUID]uuid.UUID, userEmails map[string]string) *types.RiskResult {
	var chatID *string
	var chatTitle *string
	if id, err := uuid.Parse(row.ChatID); err == nil {
		chatID = new(id.String())
		if title, ok := titles[id]; ok {
			chatTitle = &title
		}
	}

	var blockID *string
	var chatMessageID *string
	if id, err := uuid.Parse(row.ChatMessageID); err == nil {
		chatMessageID = new(id.String())
		if block, ok := blocks[id]; ok {
			blockID = new(block.String())
		}
	}

	tags := row.Tags
	if tags == nil {
		tags = []string{}
	}

	displayUserID := row.ExternalUserID
	if isMCPFindingRow(row) {
		displayUserID = userEmails[row.UserID]
		if displayUserID == "" {
			displayUserID = row.ExternalUserID
		}
		if displayUserID == "" {
			displayUserID = row.UserID
		}
	}

	return &types.RiskResult{
		ID:                 row.ID.String(),
		PolicyID:           row.RiskPolicyID,
		PolicyVersion:      row.RiskPolicyVersion,
		ExecutionID:        conv.PtrEmpty(row.ExecutionID),
		McpServerID:        conv.PtrEmpty(row.MCPServerID),
		MetaMcpServerID:    conv.PtrEmpty(row.MetaMCPServerID),
		ToolsetID:          conv.PtrEmpty(row.ToolsetID),
		ToolName:           conv.PtrEmpty(row.ToolName),
		Phase:              conv.PtrEmpty(row.Phase),
		MediationSurface:   conv.PtrEmpty(row.MediationSurface),
		McpMethod:          conv.PtrEmpty(row.MCPMethod),
		PrincipalKind:      conv.PtrEmpty(row.PrincipalKind),
		IdentityStamped:    new(row.IdentityStamped),
		EnforcementOutcome: conv.PtrEmpty(row.EnforcementOutcome),
		BlockID:            blockID,
		ChatMessageID:      chatMessageID,
		ChatContentPartID:  conv.PtrEmpty(row.ContentPartID),
		ChatID:             chatID,
		ChatTitle:          chatTitle,
		UserID:             conv.PtrEmpty(displayUserID),
		Source:             row.Source,
		RuleID:             conv.PtrEmpty(row.RuleID),
		Description:        conv.PtrEmpty(row.Description),
		Match:              nil,
		StartPos:           new(int(row.StartPos)),
		EndPos:             new(int(row.EndPos)),
		Confidence:         new(row.Confidence),
		Tags:               tags,
		Spans:              nil,
		MatchRedacted:      conv.PtrEmpty(row.MatchRedacted),
		// The ClickHouse listing only serves open findings, so a served row is
		// never suppressed; only ListDismissedRiskResults populates the
		// suppression fields, which it does after calling this mapper.
		FalsePositiveAt:  nil,
		SuppressedAt:     nil,
		SuppressedReason: nil,
		SuppressedDetail: nil,
		ExclusionID:      nil,
		// Message event time, not scan time: the Postgres listing exposes
		// message_created_at as CreatedAt, and the sort order and cursor both
		// key on it.
		CreatedAt: row.MessageCreatedAt.UTC().Format(time.RFC3339),
	}
}
