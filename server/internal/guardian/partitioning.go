package guardian

import (
	"slices"
	"strconv"
	"strings"
)

// Partition identifies the resilience state (rate limit bucket, circuit
// breaker) guarding a request. It is structured — namespace, partition
// segments, subset segments — and each part is exposed as its own telemetry
// attribute so that metrics can be filtered and grouped per dependency, per
// upstream, and per tenant. [Partition.String] is the storage identity.
//
// Segments are stored verbatim: no characters are reserved or rewritten.
// String emits a self-delimiting encoding, so distinct keys can never
// collide no matter what their segments contain.
type Partition struct {
	namespace string
	partition []string
	subset    []string
}

// String returns the key's storage identity. Every part is prefixed with its
// byte length ("pk:3:svc:11:example.com:3:443|5:org-a"), so parsing consumes
// content by length rather than by separator: the encoding is injective and
// segments may safely contain ':' (e.g. IPv6 host literals), '|', or any
// other byte. The '|' marks where subset segments begin.
func (pk Partition) String() string {
	var sb strings.Builder
	sb.WriteString("pk:")
	writeSegment(&sb, pk.namespace)
	for _, segment := range pk.partition {
		sb.WriteString(":")
		writeSegment(&sb, segment)
	}
	for i, segment := range pk.subset {
		if i == 0 {
			sb.WriteString("|")
		} else {
			sb.WriteString(":")
		}
		writeSegment(&sb, segment)
	}

	return sb.String()
}

func writeSegment(sb *strings.Builder, segment string) {
	sb.WriteString(strconv.Itoa(len(segment)))
	sb.WriteString(":")
	sb.WriteString(segment)
}

// Namespace returns the namespace the key was built with, which for keys
// built by the resilience layer is the [WithResilience] name. It is exposed
// on telemetry as the gram.resilience.namespace attribute.
func (pk Partition) Namespace() string {
	return pk.namespace
}

// Partition returns the partition segments joined with ':' for display,
// which for keys built by the resilience layer come from the client's
// [PartitionStrategy] (e.g. "api.example.com:443"). It is exposed on
// telemetry as the gram.resilience.partition attribute. Unlike
// [Partition.String], the join is not injective — it is a label, not an
// identity.
func (pk Partition) Partition() string {
	return strings.Join(pk.partition, ":")
}

// Subset returns the subset segments joined with ':' for display, which for
// keys built by the resilience layer come from [WithSubset] (e.g. a tenant
// identifier). It is exposed on telemetry as the gram.resilience.subset
// attribute; empty when the key is not subsetted. Unlike [Partition.String],
// the join is not injective — it is a label, not an identity.
func (pk Partition) Subset() string {
	return strings.Join(pk.subset, ":")
}

// NewPartition builds a key from a namespace and the partition segments that
// narrow it. The namespace is the key's primary telemetry identity (see
// [Partition.Namespace]) and doubles as its first segment.
func NewPartition(namespace string, segments ...string) Partition {
	return Partition{
		namespace: namespace,
		partition: slices.Clone(segments),
		subset:    nil,
	}
}

// WithSubset returns a copy of the key narrowed by the given subset segments.
// Multiple calls compose by appending, so a subset can only ever narrow a
// key, never merge two.
func (pk Partition) WithSubset(segments ...string) Partition {
	if len(segments) == 0 {
		return pk
	}

	merged := make([]string, 0, len(pk.subset)+len(segments))
	merged = append(merged, pk.subset...)
	merged = append(merged, segments...)
	pk.subset = merged

	return pk
}
