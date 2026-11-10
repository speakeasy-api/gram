package chatfindings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	// maxPolicyCount bounds policy metadata loaded for one project.
	maxPolicyCount = 1000
	// maxQualifyingChatCount bounds the UUID array passed back to Postgres.
	maxQualifyingChatCount = 100000
)

var (
	errPolicyLimitExceeded = errors.New("chat findings policy limit exceeded")
	errChatLimitExceeded   = errors.New("chat findings qualifying chat limit exceeded")
)

type policyReader interface {
	ListRiskFindingPolicies(context.Context, riskrepo.ListRiskFindingPoliciesParams) ([]riskrepo.ListRiskFindingPoliciesRow, error)
}

// FindingReader supplies the ClickHouse side of chat enrichment.
type FindingReader interface {
	ListChatFindingCounts(context.Context, chrepo.ChatFindingParams) ([]chrepo.ChatFindingCount, error)
	ListChatFindingAnchors(context.Context, chrepo.ChatFindingParams) ([]chrepo.ChatFindingAnchor, error)
}

// Reader combines current policy eligibility from Postgres with findings from ClickHouse.
type Reader struct {
	policies policyReader
	findings FindingReader
}

// Scope identifies one tenant project.
type Scope struct {
	// OrganizationID is the tenant identifier used by ClickHouse.
	OrganizationID string
	// ProjectID scopes policy metadata and findings.
	ProjectID uuid.UUID
}

// Project is a policy-consistent view used by one chat request.
type Project struct {
	scope    Scope
	findings FindingReader
	policies []riskrepo.ListRiskFindingPoliciesRow
}

// Anchors are the messages and content parts containing live findings.
type Anchors struct {
	// MessageIDs contains messages with live findings.
	MessageIDs []uuid.UUID
	// ContentPartIDs contains attachments with live findings.
	ContentPartIDs []uuid.UUID
}

// New creates a chat findings reader over Postgres policy metadata and ClickHouse findings.
func New(db *pgxpool.Pool, findings FindingReader) *Reader {
	if db == nil || findings == nil {
		return nil
	}
	return &Reader{policies: riskrepo.New(db), findings: findings}
}

// ForProject resolves enabled, non-deleted policies once for one request.
func (r *Reader) ForProject(ctx context.Context, scope Scope) (*Project, error) {
	if r == nil || r.policies == nil || r.findings == nil {
		return nil, errors.New("chat findings reader is unavailable")
	}
	policies, err := r.policies.ListRiskFindingPolicies(ctx, riskrepo.ListRiskFindingPoliciesParams{
		ProjectID:      scope.ProjectID,
		OrganizationID: scope.OrganizationID,
		PageLimit:      maxPolicyCount + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list chat finding policies: %w", err)
	}
	if len(policies) > maxPolicyCount {
		return nil, errPolicyLimitExceeded
	}

	active := make([]riskrepo.ListRiskFindingPoliciesRow, 0, len(policies))
	for _, policy := range policies {
		if policy.Enabled && !policy.Deleted && policy.ProjectID == scope.ProjectID && policy.OrganizationID == scope.OrganizationID {
			active = append(active, policy)
		}
	}
	return &Project{scope: scope, findings: r.findings, policies: active}, nil
}

func (p *Project) policyIDs() []string {
	ids := make([]string, 0, len(p.policies))
	for _, policy := range p.policies {
		ids = append(ids, policy.ID.String())
	}
	slices.Sort(ids)
	return ids
}

// QualifyingChatIDs returns chats with at least minCount live findings under
// eligible policies. A zero minCount means at least one.
func (p *Project) QualifyingChatIDs(ctx context.Context, minCount uint64) ([]uuid.UUID, error) {
	policyIDs := p.policyIDs()
	if len(policyIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	rows, err := p.findings.ListChatFindingCounts(ctx, chrepo.ChatFindingParams{
		OrganizationID: p.scope.OrganizationID,
		ProjectID:      p.scope.ProjectID.String(),
		PolicyIDs:      policyIDs,
		ChatIDs:        nil,
		MinCount:       minCount,
		Limit:          maxQualifyingChatCount + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list qualifying finding chats: %w", err)
	}
	if len(rows) > maxQualifyingChatCount {
		return nil, errChatLimitExceeded
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		id, err := uuid.Parse(row.ChatID)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// Counts returns live finding counts for the requested chats.
func (p *Project) Counts(ctx context.Context, chatIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	counts := make(map[uuid.UUID]int64, len(chatIDs))
	policyIDs := p.policyIDs()
	if len(chatIDs) == 0 || len(policyIDs) == 0 {
		return counts, nil
	}

	ids := make([]string, len(chatIDs))
	for i, id := range chatIDs {
		ids[i] = id.String()
	}
	rows, err := p.findings.ListChatFindingCounts(ctx, chrepo.ChatFindingParams{
		OrganizationID: p.scope.OrganizationID,
		ProjectID:      p.scope.ProjectID.String(),
		PolicyIDs:      policyIDs,
		ChatIDs:        ids,
		MinCount:       0,
		Limit:          uint64(len(ids)),
	})
	if err != nil {
		return nil, fmt.Errorf("count chat findings: %w", err)
	}
	for _, row := range rows {
		if row.Count > math.MaxInt64 {
			return nil, errors.New("chat finding count exceeds int64")
		}
		id, err := uuid.Parse(row.ChatID)
		if err == nil {
			counts[id] = int64(row.Count)
		}
	}
	return counts, nil
}

// FindingAnchors returns live message and content part anchors for one chat.
func (p *Project) FindingAnchors(ctx context.Context, chatID uuid.UUID) (Anchors, error) {
	policyIDs := p.policyIDs()
	if len(policyIDs) == 0 {
		return Anchors{MessageIDs: []uuid.UUID{}, ContentPartIDs: []uuid.UUID{}}, nil
	}
	rows, err := p.findings.ListChatFindingAnchors(ctx, chrepo.ChatFindingParams{
		OrganizationID: p.scope.OrganizationID,
		ProjectID:      p.scope.ProjectID.String(),
		PolicyIDs:      policyIDs,
		ChatIDs:        []string{chatID.String()},
		MinCount:       0,
		Limit:          0,
	})
	if err != nil {
		return Anchors{}, fmt.Errorf("list chat finding anchors: %w", err)
	}

	messageSet := make(map[uuid.UUID]struct{}, len(rows))
	partSet := make(map[uuid.UUID]struct{}, len(rows))
	for _, row := range rows {
		if id, err := uuid.Parse(row.ChatMessageID); err == nil {
			messageSet[id] = struct{}{}
		}
		if id, err := uuid.Parse(row.ContentPartID); err == nil {
			partSet[id] = struct{}{}
		}
	}
	anchors := Anchors{
		MessageIDs:     make([]uuid.UUID, 0, len(messageSet)),
		ContentPartIDs: make([]uuid.UUID, 0, len(partSet)),
	}
	for id := range messageSet {
		anchors.MessageIDs = append(anchors.MessageIDs, id)
	}
	for id := range partSet {
		anchors.ContentPartIDs = append(anchors.ContentPartIDs, id)
	}
	slices.SortFunc(anchors.MessageIDs, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	slices.SortFunc(anchors.ContentPartIDs, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return anchors, nil
}
