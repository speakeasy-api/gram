// Package partition identifies the Speakeasy partition a process runs in. A
// partition is an independent production deployment with its own data stores
// and hosts; nothing at runtime crosses between partitions. Organizations are
// assigned to exactly one partition.
package partition

import (
	"fmt"
	"slices"
	"strings"
)

// Partition names one partition. The set is closed: adding a partition is a
// code change, so a typo in configuration fails at startup instead of
// silently creating a new partition.
type Partition string

const (
	// US is the original production deployment. Organizations without a
	// recorded partition belong to it.
	US Partition = "us"

	// EU is the deployment that keeps customer data in EU locations.
	EU Partition = "eu"
)

// All lists every known partition in a stable order.
func All() []Partition {
	return []Partition{US, EU}
}

// Parse returns the partition named by raw. Names are case sensitive and must
// be one of All.
func Parse(raw string) (Partition, error) {
	p := Partition(raw)
	if !slices.Contains(All(), p) {
		return "", fmt.Errorf("unknown partition %q: must be one of %s", raw, strings.Join(Names(), ", "))
	}

	return p, nil
}

func (p Partition) String() string {
	return string(p)
}

// Names lists the name of every known partition, in the order of All.
func Names() []string {
	names := make([]string, 0, len(All()))
	for _, p := range All() {
		names = append(names, p.String())
	}

	return names
}
