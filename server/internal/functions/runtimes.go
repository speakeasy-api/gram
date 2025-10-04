package functions

import (
	"fmt"
	"maps"
	"slices"
)

type Runtime string

const (
	RuntimeNodeJS22  Runtime = "nodejs:22"
	RuntimePython312 Runtime = "python:3.12"
)

type Runtimes map[Runtime]struct{}

func (r Runtimes) String() string {
	return fmt.Sprintf("%v", slices.Sorted(maps.Keys(supportedRuntimes)))
}

func SupportedRuntimes() Runtimes {
	return Runtimes{
		RuntimeNodeJS22:  {},
		RuntimePython312: {},
	}
}

var supportedRuntimes = SupportedRuntimes()

func IsSupportedRuntime(runtime string) bool {
	_, ok := supportedRuntimes[Runtime(runtime)]
	return ok
}
