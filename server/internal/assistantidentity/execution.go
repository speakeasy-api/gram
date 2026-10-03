package assistantidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
)

const ExecutionVersion = 1

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
	ContinuationEventID string          `json:"continuation_event_id,omitempty"`
	Version             int             `json:"version"`
	Identity            Identity        `json:"identity"`
	Issuer              string          `json:"issuer"`
	ThreadID            uuid.UUID       `json:"thread_id"`
	EventID             string          `json:"event_id"`
	Mode                ExecutionMode   `json:"mode"`
	HumanUserID         string          `json:"human_user_id,omitempty"`
	FallbackReason      string          `json:"fallback_reason,omitempty"`
	Ceiling             CeilingSnapshot `json:"ceiling"`
}

func (e Execution) Check() error {
	i := e.Identity
	if e.Version != ExecutionVersion || e.Issuer == "" || e.EventID == "" || e.ThreadID == uuid.Nil || i.OrganizationID == "" || i.ProjectID == uuid.Nil || i.AssistantID == uuid.Nil || i.AgentID == uuid.Nil || i.TriggerID == uuid.Nil || i.IssuerID == uuid.Nil || i.Subject == "" || i.AssistantGeneration <= 0 || i.TriggerGeneration <= 0 {
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
	if err := e.Check(); err != nil {
		return err
	}
	if s == nil || e.Issuer != s.issuer {
		return ErrInvalidIdentity
	}
	return s.Validate(ctx, db, e.Identity)
}

func (s *Service) Issuer() string { return s.issuer }

// AdmitExecution is deliberately closed until mode-aware positive admission is
// implemented. Keeping the gate in code prevents a configuration switch from
// activating a workload with only identity validation or an issuance ceiling.
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
