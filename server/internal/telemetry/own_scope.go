package telemetry

import (
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// OwnScopeFor turns an authorization read scope into the query predicate the
// telemetry repository applies. nil means the caller reads the whole project.
// identityOrg is the canonical organization the hook rows are keyed under.
func OwnScopeFor(scope authz.ReadScope, identityOrg string) *repo.OwnScope {
	if scope.Unrestricted() {
		return nil
	}
	agentIDs := make([]string, 0, len(scope.AgentIDs))
	for _, id := range scope.AgentIDs {
		agentIDs = append(agentIDs, id.String())
	}
	return &repo.OwnScope{
		UserID:      scope.UserID,
		UserEmail:   scope.UserEmail,
		AgentIDs:    agentIDs,
		IdentityOrg: identityOrg,
	}
}
