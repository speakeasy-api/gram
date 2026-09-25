package metamcp

// DiscoveryMode controls the tool catalog exposed by a stored gateway.
type DiscoveryMode string

const (
	DiscoveryModeProgressive DiscoveryMode = "progressive"
	DiscoveryModeDirect      DiscoveryMode = "direct"
)

// Valid reports whether the mode is supported.
func (m DiscoveryMode) Valid() bool {
	return m == DiscoveryModeProgressive || m == DiscoveryModeDirect
}

// ResolveDiscoveryMode preserves Progressive discovery for unset defaults.
func ResolveDiscoveryMode(value string) DiscoveryMode {
	if DiscoveryMode(value) == DiscoveryModeDirect {
		return DiscoveryModeDirect
	}
	return DiscoveryModeProgressive
}
