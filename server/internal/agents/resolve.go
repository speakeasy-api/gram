package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	// ErrPrincipalInvalid reports input that cannot identify an agent principal.
	ErrPrincipalInvalid = errors.New("agent principal invalid")
	// ErrPrincipalNotFound reports that an agent does not resolve in the tenant.
	ErrPrincipalNotFound = errors.New("agent principal not found")
)

// ResolvePrincipal resolves a non-deleted agent by both organization and ID.
// Missing, deleted, and cross-organization agents intentionally share one error.
func ResolvePrincipal(ctx context.Context, db repo.DBTX, organizationID string, principal urn.Principal) (repo.Agent, error) {
	if organizationID == "" {
		return repo.Agent{}, fmt.Errorf("organization id is required")
	}
	parsed, err := urn.ParsePrincipal(principal.String())
	if err != nil || parsed.Type != urn.PrincipalTypeAgent {
		return repo.Agent{}, fmt.Errorf("%w: unsupported or malformed agent principal", ErrPrincipalInvalid)
	}

	agentID, err := uuid.Parse(parsed.ID)
	if err != nil {
		return repo.Agent{}, fmt.Errorf("%w: malformed id", ErrPrincipalInvalid)
	}

	agent, err := repo.New(db).GetAgentByID(ctx, repo.GetAgentByIDParams{
		OrganizationID: organizationID,
		ID:             agentID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Agent{}, ErrPrincipalNotFound
		}
		return repo.Agent{}, fmt.Errorf("resolve agent principal: %w", err)
	}

	return agent, nil
}
