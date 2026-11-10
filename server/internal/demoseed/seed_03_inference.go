package demoseed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

const demoInferenceScope = "inference:execute"

func inferenceGrantSelectors(projectID uuid.UUID) ([]byte, error) {
	selectors, err := json.Marshal(map[string]string{
		"resource_kind": "inference", "resource_id": "*", "project_id": projectID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("encode inference grant selectors: %w", err)
	}
	return selectors, nil
}

// This fictional platform cannot issue usable tokens. Only its dedicated agent
// and owner receive inference permission; existing MCP agents retain their grants.
// The normal organization-scoped workload and agent cleanup removes these rows.
func (s *seeder) seedDeploymentInference(ctx context.Context) error {
	org, proj := s.orgID(), s.projectID()
	agent, issuer := s.det("inference-agent"), s.det("inference-workload-issuer")
	owner := s.userID("priya")
	const subject = "service:acme:summary-worker"
	if err := s.q.DeployInsertAgent(ctx, repo.DeployInsertAgentParams{
		ID: agent, OrganizationID: org, ProjectID: proj, OwnerUserID: owner, Name: "Inference summary worker",
	}); err != nil {
		return fmt.Errorf("insert demo inference agent: %w", err)
	}
	if err := s.q.DeployInsertOrgWorkloadIssuer(ctx, repo.DeployInsertOrgWorkloadIssuerParams{
		ID: issuer, OrganizationID: org, Name: "Acme Inference Platform",
		Description: "Summary workers exchanging platform identity for project-scoped inference access.",
		Tags:        []string{"inference", "workers"}, Issuer: "https://inference.example.com",
		JwksUri: "https://inference.example.com/.well-known/jwks.json", AllowWildcardAdmission: false,
		AllowAssertionReuse: false,
	}); err != nil {
		return fmt.Errorf("insert demo inference workload issuer: %w", err)
	}
	if err := s.q.DeployInsertWorkloadAdmission(ctx, repo.DeployInsertWorkloadAdmissionParams{
		ID: s.det("inference-workload-admission"), OrganizationID: org,
		ProjectID: uuid.NullUUID{UUID: proj, Valid: true}, WorkloadIssuerID: issuer,
		Subject: subject, MatchKind: "exact", Name: "Summary inference worker", Tags: []string{"inference"},
	}); err != nil {
		return fmt.Errorf("insert demo inference workload admission: %w", err)
	}
	if err := s.q.DeployInsertWorkloadAssignment(ctx, repo.DeployInsertWorkloadAssignmentParams{
		ID: s.det("inference-workload-assignment"), OrganizationID: org, WorkloadIssuerID: issuer,
		Subject: subject, MatchKind: "exact", AgentID: agent,
	}); err != nil {
		return fmt.Errorf("insert demo inference workload assignment: %w", err)
	}
	selectors, err := inferenceGrantSelectors(proj)
	if err != nil {
		return fmt.Errorf("encode demo inference selectors: %w", err)
	}
	// Both the agent ceiling and the owner's effective access must permit inference.
	for _, g := range []struct{ name, principal string }{
		{"inference-agent-grant", "agent:" + agent.String()},
		{"inference-owner-grant", "user:" + owner},
	} {
		if err := s.q.DeployInsertPrincipalGrant(ctx, repo.DeployInsertPrincipalGrantParams{
			ID: s.det(g.name), OrganizationID: org, PrincipalUrn: g.principal,
			Scope: demoInferenceScope, Selectors: selectors,
		}); err != nil {
			return fmt.Errorf("insert demo %s: %w", g.name, err)
		}
	}
	return nil
}
