package skills

import (
	"context"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
)

// SkillsService is the read-only subset of the skills management service used
// by the project's managed assistant.
type SkillsService interface {
	List(context.Context, *genskills.ListPayload) (*genskills.ListSkillsResult, error)
	Get(context.Context, *genskills.GetPayload) (*genskills.GetSkillResult, error)
	ListVersions(context.Context, *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error)
	ListDistributions(context.Context, *genskills.ListDistributionsPayload) (*genskills.ListSkillDistributionsResult, error)
}
