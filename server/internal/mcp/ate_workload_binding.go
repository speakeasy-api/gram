package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

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

// admitATEWorkloadGrantInTx must run in the issuance READ COMMITTED transaction,
// before its issuer lock. All fences are retained until the session commits.
// Lock order matches lifecycle writers: assistant advisory, assistant row,
// runtime row, workload issuer, then agent. Initial reads only locate the fences;
// assignment, metadata and identity are checked again under those fences.
func admitATEWorkloadGrantInTx(ctx context.Context, tx pgx.Tx, tenancy workloadTenancy, presented presentedWorkload) error {
	runtimeID, managed := ateManagedRuntimeID(presented.subject)
	if !managed {
		return nil
	}
	if runtimeID == uuid.Nil || tenancy.OrganizationID == "" || !presented.signatureVerified || presented.ateClaims == nil {
		return errWorkloadNotAdmitted
	}
	issuer, subject := presented.issuerID, presented.subject
	params := workloadidentity.AssignmentParams{OrganizationID: tenancy.OrganizationID, WorkloadIssuerID: issuer, Subject: subject}
	agentID, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, tx, params)
	if err != nil {
		return fmt.Errorf("locate ATE workload assignment: %w", err)
	}
	if !assigned {
		return errWorkloadNotAdmitted
	}
	agent, err := agents.ResolvePrincipal(ctx, tx, tenancy.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
	if errors.Is(err, agents.ErrPrincipalNotFound) {
		return errWorkloadNotAdmitted
	}
	if err != nil {
		return fmt.Errorf("locate ATE workload project: %w", err)
	}
	if !agent.ProjectID.Valid || agent.ProjectID.UUID == uuid.Nil {
		return errWorkloadNotAdmitted
	}
	projectID := agent.ProjectID.UUID
	q := assistantrepo.New(tx)
	hint, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errWorkloadNotAdmitted
	}
	if err != nil {
		return fmt.Errorf("locate ATE workload runtime: %w", err)
	}
	if err := q.AcquireAssistantAdvisoryLock(ctx, hint.AssistantID.String()); err != nil {
		return fmt.Errorf("fence ATE assistant lifecycle: %w", err)
	}
	if _, err := identityrepo.New(tx).LockAssistant(ctx, identityrepo.LockAssistantParams{ProjectID: projectID, AssistantID: hint.AssistantID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errWorkloadNotAdmitted
		}
		return fmt.Errorf("lock ATE assistant: %w", err)
	}
	r, err := q.GetAssistantRuntimeForAdmission(ctx, assistantrepo.GetAssistantRuntimeForAdmissionParams{ID: runtimeID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errWorkloadNotAdmitted
	}
	if err != nil {
		return fmt.Errorf("lock ATE workload runtime: %w", err)
	}
	if r.AssistantID != hint.AssistantID {
		return errWorkloadNotAdmitted
	}
	var policyProject *uuid.UUID
	if tenancy.ProjectID != uuid.Nil {
		policyProject = &tenancy.ProjectID
	}
	if !presented.assertionPolicyChecked {
		return errWorkloadNotAdmitted
	}
	if err := lockWorkloadAssertionPolicy(ctx, tx, tenancy.OrganizationID, policyProject, presented); err != nil {
		return err
	}
	currentAgent, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, tx, params)
	if err != nil {
		return fmt.Errorf("recheck ATE workload assignment: %w", err)
	}
	if !assigned || currentAgent != agentID {
		return errWorkloadNotAdmitted
	}
	agent, err = agentrepo.New(tx).LockAgentIncludingDeleted(ctx, agentrepo.LockAgentIncludingDeletedParams{OrganizationID: tenancy.OrganizationID, ID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errWorkloadNotAdmitted
	}
	if err != nil {
		return fmt.Errorf("lock ATE workload agent: %w", err)
	}
	if !ateWorkloadAgentActive(agent) || !agent.ProjectID.Valid || agent.ProjectID.UUID != projectID {
		return errWorkloadNotAdmitted
	}
	identity, err := resolveATEStoredBinding(r, tenancy, agentID, issuer, subject, *presented.ateClaims)
	if err != nil {
		return err
	}
	a, err := q.GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{AssistantID: r.AssistantID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errWorkloadNotAdmitted
	}
	if err != nil {
		return fmt.Errorf("read locked ATE assistant: %w", err)
	}
	if a.OrganizationID != tenancy.OrganizationID || a.RuntimeVersion != 3 || a.Status != "active" || a.DeletedAt.Valid {
		return errWorkloadNotAdmitted
	}
	currentIdentity, err := assistantidentity.ResolveRuntimeLocked(ctx, tx, tenancy.OrganizationID, projectID, r.AssistantID)
	if err != nil {
		if errors.Is(err, assistantidentity.ErrInvalidIdentity) || errors.Is(err, assistantidentity.ErrNotFound) {
			return fmt.Errorf("validate locked ATE identity: %w", errors.Join(errWorkloadNotAdmitted, err))
		}
		return fmt.Errorf("resolve locked ATE identity: %w", err)
	}
	if currentIdentity != identity {
		return errWorkloadNotAdmitted
	}
	return nil
}

func ateWorkloadAgentActive(agent agentrepo.Agent) bool {
	return agents.DeriveLifecycle(agent) == agents.LifecycleActive && !agent.OwnerReassignmentRequiredAt.Valid
}
