package assistantidentity

import "errors"

// Rollout is deployment configuration, never a caller-selected authorization
// bypass. Zero values preserve the existing rollout. Configure every serving
// tier and worker consistently; disabling a gate never erases binding history.
type Rollout struct {
	DisableProvisioning    bool
	DisableExecution       bool
	DisableSlackDelegation bool
}

var ErrRolloutDisabled = errors.New("assistant workload execution temporarily disabled")
var ErrProvisioningDisabled = errors.New("assistant identity provisioning temporarily disabled")

func (s *Service) Rollout() Rollout { return s.rollout }

// CheckRollout is applied at token issuance and use. Disabling delegation does
// not reinterpret an already selected human as an autonomous workload.
func (s *Service) CheckRollout(e Execution) error {
	if s == nil {
		return ErrInvalidIdentity
	}
	if s.rollout.DisableExecution || (s.rollout.DisableSlackDelegation && e.Slack != nil) {
		return ErrRolloutDisabled
	}
	return nil
}
