package platformmcp

import (
	"context"
)

// AssistantIdentityManagement is the assistants management API behind the
// assistant tools. Each tool asserts the narrower method set it calls, so the
// dashboard's authorized endpoints remain the only path to a change.
type AssistantIdentityManagement any

// AssistantIdentityService backs the assistant configuration tools.
type AssistantIdentityService struct {
	management     AssistantIdentityManagement
	resolveProject func(context.Context, string, FindMCPInput) (ResolvedProject, error)
	organizations  OrganizationSlugResolver
}

// NewAssistantIdentityService resolves the named project through the same
// inventory read the other project-scoped tools use.
func NewAssistantIdentityService(management AssistantIdentityManagement, projects *PostgresReader) *AssistantIdentityService {
	return &AssistantIdentityService{
		management:     management,
		resolveProject: projects.resolveInventoryProject,
		organizations:  NewPostgresOrganizationSlugResolver(projects.db),
	}
}

func registerAssistantIdentityTool(reg *Registrar, service *AssistantIdentityService) {
	registerAssistantRuntimeTool(reg, service)
	registerAssistantReasoningEffortTool(reg, service)
	registerAssistantUpstreamCredentialTool(reg, service)
}
