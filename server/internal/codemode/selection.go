package codemode

// SelectionGate checks deployment-wide runtime availability for new settings
// and connection overrides. It is independent of settings authorization.
type SelectionGate func() bool

// Allows fails closed when the new-selection gate has not been configured.
func (g SelectionGate) Allows() bool {
	return g != nil && g()
}
