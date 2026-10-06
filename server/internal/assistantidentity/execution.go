package assistantidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
)

const ExecutionVersion = 1

// Execution describes one assistant invocation acting as its agent: the live
// workload identity of the trigger that started it, the user the turn acts
// for, and the ceiling derived from the agent's policy when it was minted. It
// is token content, not a grant; every use revalidates it against live state.
type Execution struct {
	Version     int             `json:"version"`
	Identity    Identity        `json:"identity"`
	Issuer      string          `json:"issuer"`
	ThreadID    uuid.UUID       `json:"thread_id"`
	EventID     string          `json:"event_id"`
	HumanUserID string          `json:"human_user_id"`
	Ceiling     CeilingSnapshot `json:"ceiling"`
}

// Check validates the envelope's shape and that the ceiling bytes match their
// digest under the ceiling's canonical encoding.
func (e Execution) Check() error {
	i := e.Identity
	if e.Version != ExecutionVersion || e.Issuer == "" || e.EventID == "" || e.ThreadID == uuid.Nil || e.HumanUserID == "" ||
		i.OrganizationID == "" || i.ProjectID == uuid.Nil || i.AssistantID == uuid.Nil || i.AgentID == uuid.Nil ||
		i.TriggerID == uuid.Nil || i.IssuerID == uuid.Nil || i.Subject == "" {
		return ErrInvalidIdentity
	}
	if e.Ceiling.EncodingVersion <= 0 || len(e.Ceiling.Policy) == 0 || e.Ceiling.Digest == "" {
		return ErrInvalidIdentity
	}
	// JSONB storage may rewrite whitespace or key order, so hash the policy's
	// canonical encoding rather than the bytes as given.
	policy, err := runtimepolicy.DecodeDelegatedPolicy(e.Ceiling.EncodingVersion, e.Ceiling.Policy)
	if err != nil {
		return ErrInvalidIdentity
	}
	canonical, err := runtimepolicy.EncodeDelegatedPolicy(e.Ceiling.EncodingVersion, policy)
	if err != nil {
		return ErrInvalidIdentity
	}
	digest := sha256.Sum256(canonical)
	if hex.EncodeToString(digest[:]) != e.Ceiling.Digest {
		return ErrInvalidIdentity
	}
	return nil
}

// ValidateExecution reports ErrInvalidIdentity unless e names this
// deployment's issuer and its identity still resolves exactly. The token
// issuer checks the envelope's shape. Storage failures are returned unchanged
// so callers can retry them.
func (s *Service) ValidateExecution(ctx context.Context, db DB, e Execution) error {
	if e.Issuer != s.issuer {
		return ErrInvalidIdentity
	}
	return s.Validate(ctx, db, e.Identity)
}

func (s *Service) Issuer() string { return s.issuer }
