package metamcp

// DiscoveryMode controls the tool catalog exposed by a stored gateway.
type DiscoveryMode string

const (
	DiscoveryModeProgressive DiscoveryMode = "progressive"
	DiscoveryModeDirect      DiscoveryMode = "direct"
	// DiscoveryModeCode exposes one Python execute tool over a scoped host bridge.
	DiscoveryModeCode DiscoveryMode = "code_mode"
)

// Valid reports whether the mode is supported.
func (m DiscoveryMode) Valid() bool {
	return m == DiscoveryModeProgressive || m == DiscoveryModeDirect || m == DiscoveryModeCode
}

// ResolveDiscoveryMode preserves Progressive discovery for unset defaults.
func ResolveDiscoveryMode(value string) DiscoveryMode {
	if value == "" {
		return DiscoveryModeProgressive
	}
	return DiscoveryMode(value)
}
