package riskfindings

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

type findingMatchKey struct {
	chatMessageID     string
	contentPartID     string
	riskPolicyID      string
	riskPolicyVersion int64
	source            string
	ruleID            string
	startPos          int32
	endPos            int32
}

func (r FindingRow) matchKey() findingMatchKey {
	return findingMatchKey{
		chatMessageID:     r.ChatMessageID,
		contentPartID:     r.ContentPartID,
		riskPolicyID:      r.RiskPolicyID,
		riskPolicyVersion: r.RiskPolicyVersion,
		source:            r.Source,
		ruleID:            r.RuleID,
		startPos:          r.StartPos,
		endPos:            r.EndPos,
	}
}

type latestFinding struct {
	id              uuid.UUID
	insertedAt      time.Time
	eventKind       string
	key             findingMatchKey
	excludedAt      *time.Time
	exclusionID     *uuid.UUID
	falsePositiveAt *time.Time
}

type lookupScope struct {
	organizationID string
	projectID      string
	from           time.Time
	to             time.Time
	sources        map[string]struct{}
}

type stateCopyPlan struct {
	organizationID string
	projectID      string
	targetID       uuid.UUID
	state          chrepo.BackfillSuppressionState
}

func (s *Sink) flushGapFill(ctx context.Context, rows []FindingRow) error {
	existing, err := s.lookupExisting(ctx, rows)
	if err != nil {
		return err
	}

	toInsert := make([]FindingRow, 0, len(rows))
	statePlans := make(map[uuid.UUID]stateCopyPlan)
	batchStats := make(map[string]SourceStats)

	for _, row := range rows {
		current, byID := existing.byID[row.ID]
		if byID {
			stats := batchStats[row.Source]
			stats.PresentByID++
			batchStats[row.Source] = stats
		} else {
			current = existing.byKey[row.matchKey()]
			if current == nil {
				toInsert = append(toInsert, row)
				stats := batchStats[row.Source]
				stats.Inserted++
				batchStats[row.Source] = stats
				continue
			}
			stats := batchStats[row.Source]
			stats.MatchedByKey++
			batchStats[row.Source] = stats
		}

		state, missing := missingState(current, row)
		if !missing {
			continue
		}
		if _, planned := statePlans[current.id]; planned {
			continue
		}
		if state == nil {
			stats := batchStats[row.Source]
			stats.SkippedCHNewer++
			batchStats[row.Source] = stats
			continue
		}
		statePlans[current.id] = stateCopyPlan{
			organizationID: row.OrganizationID,
			projectID:      row.ProjectID,
			targetID:       current.id,
			state:          *state,
		}
		stats := batchStats[row.Source]
		stats.StateCopies++
		batchStats[row.Source] = stats
	}

	if !s.dryRun {
		if err := s.insertRows(ctx, toInsert); err != nil {
			return err
		}
		if err := s.appendStateCopies(ctx, statePlans); err != nil {
			return err
		}
	}

	s.inserted += int64(len(toInsert))
	for source, delta := range batchStats {
		stats := s.bySource[source]
		stats.PresentByID += delta.PresentByID
		stats.MatchedByKey += delta.MatchedByKey
		stats.StateCopies += delta.StateCopies
		stats.SkippedCHNewer += delta.SkippedCHNewer
		stats.Inserted += delta.Inserted
		s.bySource[source] = stats
	}
	return nil
}

// missingState reports whether Postgres holds suppression state that the
// latest ClickHouse copy lacks. A nil state with missing=true means
// ClickHouse state wins and nothing is appended.
func missingState(current *latestFinding, desired FindingRow) (*chrepo.BackfillSuppressionState, bool) {
	falsePositiveAt := desired.FalsePositiveAt
	if falsePositiveAt == nil {
		falsePositiveAt = current.falsePositiveAt
	}

	var markedAt time.Time
	switch {
	case desired.ExcludedAt != nil && desired.ExclusionID != nil &&
		(current.excludedAt == nil || current.exclusionID == nil):
		markedAt = *desired.ExcludedAt
	case desired.FalsePositiveAt != nil && current.falsePositiveAt == nil && current.excludedAt == nil:
		markedAt = *desired.FalsePositiveAt
	case desired.FalsePositiveAt != nil && current.falsePositiveAt == nil &&
		current.excludedAt != nil && current.exclusionID != nil:
		// A rule exclusion carries false_positive_at through apply and
		// reversal, and no live path clears it while the rule holds, so the
		// missing dismissal is copied under the current exclusion.
		return &chrepo.BackfillSuppressionState{
			ExcludedAt:      current.excludedAt,
			ExclusionID:     current.exclusionID,
			FalsePositiveAt: falsePositiveAt,
			MarkedAt:        &current.insertedAt,
		}, true
	default:
		return nil, false
	}

	if clickHouseStateWins(current, markedAt) {
		return nil, true
	}
	return &chrepo.BackfillSuppressionState{
		ExcludedAt:      desired.ExcludedAt,
		ExclusionID:     desired.ExclusionID,
		FalsePositiveAt: falsePositiveAt,
		MarkedAt:        nil,
	}, true
}

func clickHouseStateWins(current *latestFinding, postgresMarkedAt time.Time) bool {
	if current.eventKind == chrepo.EventKindUnsuppression {
		return true
	}
	return current.eventKind == chrepo.EventKindSuppression && current.insertedAt.After(postgresMarkedAt)
}

func (s *Sink) appendStateCopies(ctx context.Context, plans map[uuid.UUID]stateCopyPlan) error {
	type group struct {
		organizationID string
		projectID      string
		copies         []chrepo.BackfillSuppressionCopy
	}
	groups := make(map[string]*group)
	for _, plan := range plans {
		key := plan.organizationID + "\x00" + plan.projectID
		current := groups[key]
		if current == nil {
			current = &group{organizationID: plan.organizationID, projectID: plan.projectID, copies: nil}
			groups[key] = current
		}
		current.copies = append(current.copies, chrepo.BackfillSuppressionCopy{ID: plan.targetID, State: plan.state})
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	queries := chrepo.New(s.conn)
	insertedAt := s.now().UTC()
	for i, key := range keys {
		current := groups[key]
		sort.Slice(current.copies, func(i, j int) bool {
			return current.copies[i].ID.String() < current.copies[j].ID.String()
		})
		if err := queries.AppendBackfillSuppressions(
			ctx,
			current.organizationID,
			current.projectID,
			chrepo.FormatCHTime(insertedAt.Add(time.Duration(i)*time.Nanosecond)),
			current.copies,
		); err != nil {
			return fmt.Errorf("append state copies for project %s: %w", current.projectID, err)
		}
	}
	return nil
}

type existingFindings struct {
	byID  map[uuid.UUID]*latestFinding
	byKey map[findingMatchKey]*latestFinding
}

func (s *Sink) lookupExisting(ctx context.Context, candidates []FindingRow) (existingFindings, error) {
	scopes := make(map[string]*lookupScope)
	for _, row := range candidates {
		key := row.OrganizationID + "\x00" + row.ProjectID
		scope := scopes[key]
		if scope == nil {
			scope = &lookupScope{
				organizationID: row.OrganizationID,
				projectID:      row.ProjectID,
				from:           row.CreatedAt,
				to:             row.CreatedAt,
				sources:        make(map[string]struct{}),
			}
			scopes[key] = scope
		}
		if row.CreatedAt.Before(scope.from) {
			scope.from = row.CreatedAt
		}
		if row.CreatedAt.After(scope.to) {
			scope.to = row.CreatedAt
		}
		scope.sources[row.Source] = struct{}{}
	}

	result := existingFindings{
		byID:  make(map[uuid.UUID]*latestFinding),
		byKey: make(map[findingMatchKey]*latestFinding),
	}
	keys := make([]string, 0, len(scopes))
	for key := range scopes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := s.queryScope(ctx, scopes[key], &result); err != nil {
			return existingFindings{}, err
		}
	}
	return result, nil
}

func (s *Sink) queryScope(ctx context.Context, scope *lookupScope, result *existingFindings) error {
	sources := make([]string, 0, len(scope.sources))
	for source := range scope.sources {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(sources)), ",")
	from := scope.from.UTC().Truncate(24 * time.Hour)
	to := scope.to.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	query := `
SELECT id, inserted_at, event_kind, chat_message_id, content_part_id,
       risk_policy_id, risk_policy_version, source, rule_id, start_pos, end_pos,
       excluded_at, exclusion_id, false_positive_at
FROM (
    SELECT id, inserted_at, event_kind, chat_message_id, content_part_id,
           risk_policy_id, risk_policy_version, source, rule_id, start_pos, end_pos,
           excluded_at, exclusion_id, false_positive_at,
           ROW_NUMBER() OVER (
               PARTITION BY id
               ORDER BY (event_kind IN ('suppression', 'unsuppression')) DESC, inserted_at DESC
           ) AS rn
    FROM risk_findings
    WHERE organization_id = ? AND project_id = ? AND shadow = 0
      AND created_at >= toDateTime64(?, 9) AND created_at < toDateTime64(?, 9)
      AND source IN (` + placeholders + `)
)
WHERE rn = 1`
	args := make([]any, 0, 4+len(sources))
	args = append(args,
		scope.organizationID,
		scope.projectID,
		chrepo.FormatCHTime(from),
		chrepo.FormatCHTime(to),
	)
	for _, source := range sources {
		args = append(args, source)
	}

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query existing findings for project %s: %w", scope.projectID, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		current := new(latestFinding)
		if err := rows.Scan(
			&current.id,
			&current.insertedAt,
			&current.eventKind,
			&current.key.chatMessageID,
			&current.key.contentPartID,
			&current.key.riskPolicyID,
			&current.key.riskPolicyVersion,
			&current.key.source,
			&current.key.ruleID,
			&current.key.startPos,
			&current.key.endPos,
			&current.excludedAt,
			&current.exclusionID,
			&current.falsePositiveAt,
		); err != nil {
			return fmt.Errorf("scan existing finding: %w", err)
		}
		result.byID[current.id] = current
		previous := result.byKey[current.key]
		if previous == nil || current.insertedAt.After(previous.insertedAt) ||
			(current.insertedAt.Equal(previous.insertedAt) && current.id.String() > previous.id.String()) {
			result.byKey[current.key] = current
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read existing findings: %w", err)
	}
	return nil
}
