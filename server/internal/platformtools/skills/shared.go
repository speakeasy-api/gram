package skills

import (
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// LoadOption configures the skills load tool.
type LoadOption func(*Load)

// WithEfficacySignaler attaches the efficacy wake to the load tool. Without it
// the tool records activations exactly as before and emits no wakes.
func WithEfficacySignaler(signaler efficacy.Signaler) LoadOption {
	return func(t *Load) { t.efficacySignaler = signaler }
}
