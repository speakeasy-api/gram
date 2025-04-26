package packages

import (
	"errors"
	"fmt"
	"math"

	"github.com/Masterminds/semver/v3"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/packages/repo"
)

type Semver struct {
	Valid      bool
	Major      int16
	Minor      int16
	Patch      int16
	Prerelease string
	Build      string
}

func (s Semver) String() string {
	out := fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
	if s.Prerelease != "" {
		out += fmt.Sprintf("-%s", s.Prerelease)
	}
	if s.Build != "" {
		out += fmt.Sprintf("+%s", s.Build)
	}
	return out
}

func ParseSemver(s string) (Semver, error) {
	v, err := semver.NewVersion(s)
	if err != nil {
		return Semver{}, err
	}

	parsedMajor, parsedMinor, parsedPatch := v.Major(), v.Minor(), v.Patch()
	if parsedMajor > math.MaxInt16 || parsedMinor > math.MaxInt16 || parsedPatch > math.MaxInt16 {
		return Semver{}, errors.New("semver major, minor, or patch is too large")
	}

	return Semver{
		Valid:      true,
		Major:      int16(parsedMajor),
		Minor:      int16(parsedMinor),
		Patch:      int16(parsedPatch),
		Prerelease: v.Prerelease(),
		Build:      v.Metadata(),
	}, nil
}

func SemverFromPackageVersion(v repo.PackageVersion) Semver {
	return Semver{
		Valid:      true,
		Major:      v.Major,
		Minor:      v.Minor,
		Patch:      v.Patch,
		Prerelease: conv.PtrValOr(conv.FromPGText[string](v.Prerelease), ""),
		Build:      conv.PtrValOr(conv.FromPGText[string](v.Build), ""),
	}
}
