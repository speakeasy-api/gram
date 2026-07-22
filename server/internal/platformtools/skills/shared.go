package skills

import (
	"context"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// SkillsService is the read-only subset of the skills management service used
// by the project's managed assistant.
type SkillsService interface {
	List(context.Context, *genskills.ListPayload) (*genskills.ListSkillsResult, error)
	Get(context.Context, *genskills.GetPayload) (*genskills.GetSkillResult, error)
	ListVersions(context.Context, *genskills.ListVersionsPayload) (*genskills.ListSkillVersionsResult, error)
	ListDistributions(context.Context, *genskills.ListDistributionsPayload) (*genskills.ListSkillDistributionsResult, error)
}

// LoadOption configures the skills load tool.
type LoadOption func(*Load)

// WithEfficacySignaler attaches the efficacy wake to the load tool. Without it
// the tool records activations exactly as before and emits no wakes.
func WithEfficacySignaler(signaler efficacy.Signaler) LoadOption {
	return func(t *Load) { t.efficacySignaler = signaler }
}
