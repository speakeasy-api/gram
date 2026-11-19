// Package hookversions classifies a hooks install against the supported
// version range. The fleet views, the outdated signal to clients and the
// oncall digest all classify through Policy.Classify.
package hookversions

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

type Status string

const (
	StatusCurrent     Status = "current"
	StatusBehind      Status = "behind"
	StatusUnsupported Status = "unsupported"
	StatusUnknown     Status = "unknown"
)

// Policy holds the release Speakeasy ships and, when set, the oldest
// supported release.
type Policy struct {
	Latest  *semver.Version
	Minimum *semver.Version
}

// Classify rates an install from its binary version and the endpoint
// generation it last sent to. The retired per-tool endpoints are always
// unsupported, whatever version they report.
func (p Policy) Classify(version, endpointGeneration string) Status {
	if strings.HasPrefix(endpointGeneration, "legacy_") {
		return StatusUnsupported
	}
	if version == "" || p.Latest == nil {
		return StatusUnknown
	}
	parsed, err := semver.NewVersion(version)
	if err != nil {
		return StatusUnknown
	}
	switch {
	case p.Minimum != nil && parsed.LessThan(p.Minimum):
		return StatusUnsupported
	case parsed.LessThan(p.Latest):
		return StatusBehind
	default:
		return StatusCurrent
	}
}
