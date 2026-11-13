package authz

// Disposition describes a tool classification, not a connection property.
type Disposition string

const (
	// DispositionUnknown means no usable classification was supplied.
	DispositionUnknown Disposition = ""

	// ReadOnly describes a tool classified as nonmutating.
	ReadOnly Disposition = "read_only"

	// Destructive describes a tool classified as destructive.
	Destructive Disposition = "destructive"

	// Idempotent describes a tool classified as idempotent.
	Idempotent Disposition = "idempotent"

	// OpenWorld describes a tool classified as interacting with an open world.
	OpenWorld Disposition = "open_world"
)

// ToolFacts describe a concrete invocation, not a connection.
type ToolFacts struct {
	// Name identifies the tool being invoked.
	Name string

	// Disposition comes from the chosen classification source.
	Disposition Disposition
}
