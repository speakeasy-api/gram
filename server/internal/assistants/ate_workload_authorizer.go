package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

const (
	ateActorClaimKey          = "actor.ate.dev"
	ateWorkerForActorClaimKey = "worker-for-actor.ate.dev"
)

// ErrATEWorkloadAuthorizerRejected marks a permanent authentication/binding
// refusal for an ATE workload JWT (for example a retired/replaced actor UID,
// tampered or mismatched actor.ate.dev claim, worker-for-actor token, or
// unrecorded initiator). Callers must fail closed with 401 and never fall back
// to userless workload authority.
var ErrATEWorkloadAuthorizerRejected = errors.New("ATE workload actor rejected")

// ATEWorkloadAuthorizerRequest carries the cryptographically verified JWT
// identity and claims from Stage-1 direct-bearer workload verification.
type ATEWorkloadAuthorizerRequest struct {
	OrganizationID string
	ProjectID      uuid.UUID
	IssuerID       uuid.UUID
	Subject        string
	Claims         map[string]json.RawMessage
}

// ATEWorkloadAuthorizerResolution reports the server-verified initiating user
// and canonical thread binding for an ATE thread actor JWT.
type ATEWorkloadAuthorizerResolution struct {
	Bound            bool
	AuthorizerUserID string
	AssistantID      uuid.UUID
	ThreadID         uuid.UUID
	ChatID           uuid.UUID
}

// ATEWorkloadAuthorizer resolves the durable initiating user and thread binding
// of an ATE thread actor strictly from the verified JWT's actor.ate.dev claim
// and live assistant_ate_actors state.
type ATEWorkloadAuthorizer struct {
	db *pgxpool.Pool
}

// NewATEWorkloadAuthorizer constructs a resolver backed by db.
func NewATEWorkloadAuthorizer(db *pgxpool.Pool) *ATEWorkloadAuthorizer {
	return &ATEWorkloadAuthorizer{db: db}
}

type ateActorJWTClaim struct {
	Atespace  string `json:"atespace"`
	ActorName string `json:"actorName"`
	ActorUID  string `json:"actorUID"`
	Node      string `json:"node"`
}

// Resolve inspects the verified JWT claims and looks up the live ATE thread
// actor row by actor UID. Non-ATE workloads return Bound=false, err=nil so
// generic workload callers keep their existing behavior. Every ATE actor token
// must match a live assistant_ate_actors row and recorded initiator resolution.
func (r *ATEWorkloadAuthorizer) Resolve(ctx context.Context, req ATEWorkloadAuthorizerRequest) (ATEWorkloadAuthorizerResolution, error) {
	var zero ATEWorkloadAuthorizerResolution

	if _, isWorker := req.Claims[ateWorkerForActorClaimKey]; isWorker {
		return zero, fmt.Errorf("%w: worker-for-actor token cannot act as a thread actor", ErrATEWorkloadAuthorizerRejected)
	}
	rawActor, hasActorClaim := req.Claims[ateActorClaimKey]
	if !hasActorClaim {
		if strings.HasPrefix(req.Subject, "actor/") {
			return zero, fmt.Errorf("%w: ATE actor subject missing %s claim", ErrATEWorkloadAuthorizerRejected, ateActorClaimKey)
		}
		return zero, nil
	}

	var claim ateActorJWTClaim
	if err := json.Unmarshal(rawActor, &claim); err != nil {
		return zero, fmt.Errorf("%w: malformed %s claim: %w", ErrATEWorkloadAuthorizerRejected, ateActorClaimKey, err)
	}
	if claim.Atespace == "" || claim.ActorName == "" || claim.ActorUID == "" || claim.Node != "" {
		return zero, fmt.Errorf("%w: incomplete or invalid %s claim", ErrATEWorkloadAuthorizerRejected, ateActorClaimKey)
	}
	expectedSub := "actor/" + claim.Atespace + "/" + claim.ActorName
	if req.Subject != expectedSub {
		return zero, fmt.Errorf("%w: JWT sub does not match %s claim", ErrATEWorkloadAuthorizerRejected, ateActorClaimKey)
	}
	if r == nil || r.db == nil || req.OrganizationID == "" || req.ProjectID == uuid.Nil || req.IssuerID == uuid.Nil {
		return zero, errors.New("ATE workload authorizer is unavailable")
	}

	row, err := assistantrepo.New(r.db).GetAssistantATEActorByUIDWithChat(ctx, assistantrepo.GetAssistantATEActorByUIDWithChatParams{
		ActorUid:       claim.ActorUID,
		OrganizationID: req.OrganizationID,
		ProjectID:      req.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, fmt.Errorf("%w: no live ATE thread actor for actor UID", ErrATEWorkloadAuthorizerRejected)
	}
	if err != nil {
		return zero, fmt.Errorf("lookup live ATE thread actor by UID: %w", err)
	}

	if row.ActorName != claim.ActorName || row.ActorUid != claim.ActorUID {
		return zero, fmt.Errorf("%w: ATE actor row identity mismatch", ErrATEWorkloadAuthorizerRejected)
	}
	var binding KitWorkloadBinding
	if len(row.WorkloadBindingJson) == 0 || json.Unmarshal(row.WorkloadBindingJson, &binding) != nil {
		return zero, fmt.Errorf("%w: ATE actor workload binding missing or malformed", ErrATEWorkloadAuthorizerRejected)
	}
	if binding.IssuerID != req.IssuerID || binding.Subject != req.Subject {
		return zero, fmt.Errorf("%w: ATE actor workload binding does not match verified token", ErrATEWorkloadAuthorizerRejected)
	}

	switch row.InitiatingUserResolution {
	case ATEInitiatorNoUser:
		if row.InitiatingUserID.Valid && row.InitiatingUserID.String != "" {
			return zero, fmt.Errorf("%w: invalid no_user actor carries initiating_user_id", ErrATEWorkloadAuthorizerRejected)
		}
		return ATEWorkloadAuthorizerResolution{
			Bound:            false,
			AuthorizerUserID: "",
			AssistantID:      row.AssistantID,
			ThreadID:         row.AssistantThreadID,
			ChatID:           row.ChatID,
		}, nil
	case ATEInitiatorDashboardSender, ATEInitiatorSlackMapping, ATEInitiatorWakeRequester, ATEInitiatorMCPAuthResume:
		if !row.InitiatingUserID.Valid || row.InitiatingUserID.String == "" {
			return zero, fmt.Errorf("%w: human-initiated ATE actor missing initiating_user_id", ErrATEWorkloadAuthorizerRejected)
		}
		return ATEWorkloadAuthorizerResolution{
			Bound:            true,
			AuthorizerUserID: row.InitiatingUserID.String,
			AssistantID:      row.AssistantID,
			ThreadID:         row.AssistantThreadID,
			ChatID:           row.ChatID,
		}, nil
	default:
		return zero, fmt.Errorf("%w: ATE actor initiating user resolution %q is not authorized", ErrATEWorkloadAuthorizerRejected, row.InitiatingUserResolution)
	}
}
