package sessionquarantine

import (
	"context"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const TTL = 30 * time.Minute

type Quarantine struct {
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	SessionID      string    `json:"session_id"`
	RiskPolicyID   string    `json:"risk_policy_id"`
	RiskPolicyName string    `json:"risk_policy_name"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
}

func FromRow(row riskrepo.SessionQuarantine) Quarantine {
	riskPolicyID := ""
	if row.RiskPolicyID.Valid {
		riskPolicyID = row.RiskPolicyID.UUID.String()
	}
	createdAt := row.CreatedAt.Time
	if !row.CreatedAt.Valid {
		createdAt = row.UpdatedAt.Time
	}
	return Quarantine{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID.String(),
		SessionID:      row.SessionID,
		RiskPolicyID:   riskPolicyID,
		RiskPolicyName: row.RiskPolicyName,
		Reason:         row.Reason,
		CreatedAt:      createdAt.UTC(),
	}
}

func key(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return "session:quarantine:" + sessionID
}

func Read(ctx context.Context, cacheImpl cache.Cache, sessionID string) (*Quarantine, error) {
	k := key(sessionID)
	if cacheImpl == nil || k == "" {
		return nil, nil
	}
	var q Quarantine
	err := cacheImpl.Get(ctx, k, &q)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session quarantine circuit: %w", err)
	}
	return &q, nil
}

func Write(ctx context.Context, cacheImpl cache.Cache, q Quarantine) error {
	k := key(q.SessionID)
	if cacheImpl == nil || k == "" {
		return nil
	}
	if err := cacheImpl.Set(ctx, k, q, TTL); err != nil {
		return fmt.Errorf("write session quarantine circuit: %w", err)
	}
	return nil
}

func Delete(ctx context.Context, cacheImpl cache.Cache, sessionID string) error {
	k := key(sessionID)
	if cacheImpl == nil || k == "" {
		return nil
	}
	if err := cacheImpl.Delete(ctx, k); err != nil {
		return fmt.Errorf("delete session quarantine circuit: %w", err)
	}
	return nil
}

func DenyReason(q *Quarantine) string {
	policyName := "unknown"
	if q != nil && q.RiskPolicyName != "" {
		policyName = q.RiskPolicyName
	}
	return fmt.Sprintf("This session has been quarantined by your organization's Speakeasy risk policy %q. Contact your org admin to release it.", policyName)
}
