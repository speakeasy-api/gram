package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// This hook runs only after Verify and live subject admission. In particular,
// token claims never select the organization or project used for these reads.
type ateWorkloadBindingCheck func(context.Context, workloadTenancy, uuid.UUID, string, *jwt.JSONWebToken) error

type ateBrokerMetadata struct {
	RuntimeID   uuid.UUID                  `json:"runtime_id"`
	AssistantID uuid.UUID                  `json:"assistant_id"`
	ProjectID   uuid.UUID                  `json:"project_id"`
	Atespace    string                     `json:"atespace"`
	Threads     map[string]ateBrokerThread `json:"threads"`
}

type ateBrokerThread struct {
	ActorName string `json:"actor_name"`
	ActorUID  string `json:"actor_uid"`
	Workload  *struct {
		IssuerID uuid.UUID                         `json:"issuer_id"`
		Subject  string                            `json:"subject"`
		Identity assistantidentity.RuntimeIdentity `json:"identity"`
	} `json:"workload"`
}

type ateBrokerClaims struct {
	Actor *struct {
		Atespace  string `json:"atespace"`
		ActorName string `json:"actorName"`
		ActorUID  string `json:"actorUID"`
	} `json:"actor.ate.dev"`
	Worker         json.RawMessage `json:"worker.ate.dev"`
	WorkerForActor json.RawMessage `json:"worker-for-actor.ate.dev"`
}

// matchATEActorBinding checks a managed admission, not arbitrary user workloads.
// A worker identity is not an actor identity, even if it also carries actor data.
func matchATEActorBinding(r assistantrepo.AssistantRuntime, m ateBrokerMetadata, thread ateBrokerThread, identity assistantidentity.RuntimeIdentity, issuer uuid.UUID, subject string, claims ateBrokerClaims) bool {
	w := thread.Workload
	return r.Backend == "ate" && r.RuntimeVersion == 3 && !r.Deleted && !r.Ended && !r.EndedAt.Valid && !r.DeletedAt.Valid &&
		(r.State == "active" || r.State == "starting") &&
		m.RuntimeID == r.ID && m.ProjectID == r.ProjectID && m.AssistantID == r.AssistantID &&
		w != nil && w.IssuerID == issuer && w.Subject == subject && w.Identity == identity &&
		identity.ProjectID == r.ProjectID && identity.AssistantID == r.AssistantID &&
		m.Atespace != "" && thread.ActorName != "" && thread.ActorUID != "" &&
		subject == "actor/"+m.Atespace+"/"+thread.ActorName &&
		claims.Actor != nil && claims.Actor.Atespace == m.Atespace && claims.Actor.ActorName == thread.ActorName && claims.Actor.ActorUID == thread.ActorUID && len(claims.Worker) == 0 && len(claims.WorkerForActor) == 0
}

// The preview backend reserves this exact actor-name format. Its runtime ID
// survives metadata clearing, so a retired managed actor cannot become an
// ordinary workload by losing its stored binding.
var ateManagedSubjectPattern = regexp.MustCompile(`^actor/[^/]+/spk-kit-([0-9a-f]{32})-[0-9a-f]{16}$`)

func ateManagedRuntimeID(subject string) (uuid.UUID, bool) {
	match := ateManagedSubjectPattern.FindStringSubmatch(subject)
	if match == nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(match[1])
	return id, err == nil
}

// resolveATEStoredBinding never falls back to ordinary admission: its caller
// has already identified a reserved managed actor name independently of metadata.
func resolveATEStoredBinding(r assistantrepo.AssistantRuntime, tenancy workloadTenancy, agentID, issuer uuid.UUID, subject string, claims ateBrokerClaims) (assistantidentity.RuntimeIdentity, error) {
	var m ateBrokerMetadata
	if err := json.Unmarshal(r.BackendMetadataJson, &m); err != nil {
		return assistantidentity.RuntimeIdentity{}, errWorkloadNotAdmitted
	}
	for _, thread := range m.Threads {
		if thread.Workload == nil || thread.Workload.IssuerID != issuer || thread.Workload.Subject != subject {
			continue
		}
		identity := thread.Workload.Identity
		if identity.OrganizationID != tenancy.OrganizationID || identity.AgentID != agentID ||
			(tenancy.ProjectID != uuid.Nil && tenancy.ProjectID != r.ProjectID) ||
			!matchATEActorBinding(r, m, thread, identity, issuer, subject, claims) {
			return assistantidentity.RuntimeIdentity{}, errWorkloadNotAdmitted
		}
		return identity, nil
	}
	return assistantidentity.RuntimeIdentity{}, errWorkloadNotAdmitted
}

func ateWorkloadStoreBindingCheck(db *pgxpool.Pool) ateWorkloadBindingCheck {
	return func(ctx context.Context, tenancy workloadTenancy, issuer uuid.UUID, subject string, token *jwt.JSONWebToken) error {
		runtimeID, managed := ateManagedRuntimeID(subject)
		if !managed {
			return nil
		}
		if runtimeID == uuid.Nil || tenancy.OrganizationID == "" {
			return errWorkloadNotAdmitted
		}
		agentID, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, db, workloadidentity.AssignmentParams{OrganizationID: tenancy.OrganizationID, WorkloadIssuerID: issuer, Subject: subject})
		if err != nil {
			return err
		}
		if !assigned {
			return errWorkloadNotAdmitted
		}
		agent, err := agents.ResolvePrincipal(ctx, db, tenancy.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
		if errors.Is(err, agents.ErrPrincipalNotFound) {
			return errWorkloadNotAdmitted
		}
		if err != nil {
			return err
		}
		if !agent.ProjectID.Valid || agent.ProjectID.UUID == uuid.Nil {
			return errWorkloadNotAdmitted
		}
		q := assistantrepo.New(db)
		r, err := q.GetAssistantRuntimeForAdmission(ctx, assistantrepo.GetAssistantRuntimeForAdmissionParams{ID: runtimeID, ProjectID: agent.ProjectID.UUID})
		if errors.Is(err, pgx.ErrNoRows) {
			return errWorkloadNotAdmitted
		}
		if err != nil {
			return err
		}
		var claims ateBrokerClaims
		// The exact same immutable token bytes have already passed Verify.
		if token == nil {
			return errWorkloadNotAdmitted
		}
		if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
			return errWorkloadNotAdmitted
		}
		identity, err := resolveATEStoredBinding(r, tenancy, agentID, issuer, subject, claims)
		if err != nil {
			return err
		}
		a, err := q.GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{AssistantID: r.AssistantID, ProjectID: r.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return errWorkloadNotAdmitted
		}
		if err != nil {
			return err
		}
		if a.OrganizationID != tenancy.OrganizationID || a.RuntimeVersion != 3 || a.Status != "active" || a.DeletedAt.Valid {
			return errWorkloadNotAdmitted
		}
		if err := new(assistantidentity.Service).ValidateRuntime(ctx, db, identity); err != nil {
			if errors.Is(err, assistantidentity.ErrInvalidIdentity) || errors.Is(err, assistantidentity.ErrNotFound) {
				return errors.Join(errWorkloadNotAdmitted, err)
			}
			return err
		}
		return nil
	}
}
