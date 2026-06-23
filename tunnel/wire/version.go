package wire

import (
	"fmt"
	"strconv"
	"strings"
)

// AgentVersion is the version this build of the agent advertises on connect.
const AgentVersion = "0.1.0"

// MinSupportedAgentVersion is the floor the gateway enforces at connect time.
// Per the design there is no auto-update; stale agents are rejected with a
// clear error instead.
const MinSupportedAgentVersion = "0.1.0"

// CheckMinVersion returns nil when have >= min (semver major.minor.patch, no
// pre-release handling — sufficient for the gate). Non-numeric or short
// versions are rejected.
func CheckMinVersion(have, min string) error {
	hv, err := parseSemver(have)
	if err != nil {
		return fmt.Errorf("unparseable agent version %q: %w", have, err)
	}
	mv, err := parseSemver(min)
	if err != nil {
		return fmt.Errorf("unparseable min version %q: %w", min, err)
	}
	for i := range 3 {
		switch {
		case hv[i] > mv[i]:
			return nil
		case hv[i] < mv[i]:
			return fmt.Errorf("agent version %s is below the minimum supported %s; upgrade the agent", have, min)
		}
	}
	return nil
}

func parseSemver(s string) ([3]int, error) {
	var out [3]int
	parts := strings.SplitN(strings.TrimPrefix(s, "v"), ".", 3)
	if len(parts) != 3 {
		return out, fmt.Errorf("want major.minor.patch")
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, err
		}
		out[i] = n
	}
	return out, nil
}
