package assistantidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
)

const ExecutionVersion = 2

type ExecutionMode string

const (
	ExecutionWorkload      ExecutionMode = "WORKLOAD"
	ExecutionWorkloadHuman ExecutionMode = "WORKLOAD_HUMAN"
)

// ErrExecutionAdmissionRequired is a rollout boundary, not permission to retry
// as the assistant owner. AIM-411 supplies positive model/business admission.
var ErrExecutionAdmissionRequired = errors.New("assistant execution requires workload admission")

// Execution is trusted server metadata persisted with an event. It is not a
// grant or proof of current authority. HumanUserID never contains a machine ID.
// A continuation preserves EventID and the entire originating envelope.
type Execution struct {
	Slack               *SlackDelegation `json:"slack,omitempty"`
	ContinuationEventID string           `json:"continuation_event_id,omitempty"`
	Version             int              `json:"version"`
	Identity            Identity         `json:"identity"`
	Issuer              string           `json:"issuer"`
	ThreadID            uuid.UUID        `json:"thread_id"`
	EventID             string           `json:"event_id"`
	Mode                ExecutionMode    `json:"mode"`
	HumanUserID         string           `json:"human_user_id,omitempty"`
	FallbackReason      string           `json:"fallback_reason,omitempty"`
	Ceiling             CeilingSnapshot  `json:"ceiling"`
}

func (e Execution) Check() error {
	if d := e.Slack; d != nil {
		if e.Version < 2 || e.Mode != ExecutionWorkloadHuman || d.TeamID == "" || d.UserID == "" || d.MembershipID == uuid.Nil || d.MappingID == uuid.Nil || d.ConnectionGeneration == uuid.Nil || d.MappingRevision <= 0 {
			return ErrInvalidIdentity
		}
	}
	i := e.Identity
	if (e.Version != 1 && e.Version != ExecutionVersion) || e.Issuer == "" || e.EventID == "" || e.ThreadID == uuid.Nil || i.OrganizationID == "" || i.ProjectID == uuid.Nil || i.AssistantID == uuid.Nil || i.AgentID == uuid.Nil || i.TriggerID == uuid.Nil || i.IssuerID == uuid.Nil || i.Subject == "" || i.AssistantGeneration <= 0 || i.TriggerGeneration <= 0 {
		return ErrInvalidIdentity
	}
	switch e.Mode {
	case ExecutionWorkload:
		if e.HumanUserID != "" {
			return ErrInvalidIdentity
		}
	case ExecutionWorkloadHuman:
		if e.HumanUserID == "" || e.FallbackReason != "" {
			return ErrInvalidIdentity
		}
	default:
		return ErrInvalidIdentity
	}
	if e.Ceiling.EncodingVersion <= 0 || len(e.Ceiling.Policy) == 0 || e.Ceiling.Digest == "" {
		return ErrInvalidIdentity
	}
	// JSONB may rewrite whitespace/key ordering. Hash the policy's canonical
	// versioned encoding, not the storage representation.
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

// ValidateExecution intentionally bypasses the legacy token revocation cache.
// Passing this check establishes identity only, not model/tool authorization.
func (s *Service) ValidateExecution(ctx context.Context, db DB, e Execution) error {
	if err := s.CheckRollout(e); err != nil {
		return err
	}
	if err := e.Check(); err != nil {
		return err
	}
	if s == nil || e.Issuer != s.issuer {
		return ErrInvalidIdentity
	}
	err := s.Validate(ctx, db, e.Identity)
	// Normalize only known authority failures. Storage, network and context
	// errors must retain their identity so dispatch can retry infrastructure.
	if errors.Is(err, ErrBrokenMapping) || errors.Is(err, ErrTombstoned) || errors.Is(err, ErrActorIneligible) || errors.Is(err, ErrNotFound) {
		return ErrInvalidIdentity
	}
	return err
}

func (s *Service) Issuer() string { return s.issuer }

// AdmitExecution cannot authorize from an envelope alone. Runtime callers use
// Service.AdmitModel, which checks live workload and delegator eligibility. This
// identity-only helper remains fail-closed for callers holding only a snapshot.
func AdmitExecution(e Execution) error {
	if err := e.Check(); err != nil {
		return err
	}
	return ErrExecutionAdmissionRequired
}

// InvocationEventID identifies this delivery while EventID retains its origin.
func (e Execution) InvocationEventID() string {
	if e.ContinuationEventID != "" {
		return e.ContinuationEventID
	}
	return e.EventID
}
