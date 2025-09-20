package functions

import (
	"maps"
	"slices"
	"strings"
)

type Runtimes map[string]struct{}

func (r Runtimes) String() string {
	return strings.Join(slices.Sorted(maps.Keys(supportedRuntimes)), ", ")
}

func SupportedRuntimes() Runtimes {
	return Runtimes{
		"nodejs:22":   {},
		"python:3.12": {},
	}
}

var supportedRuntimes = SupportedRuntimes()

func IsSupportedRuntime(runtime string) bool {
	_, ok := supportedRuntimes[runtime]
	return ok
}
