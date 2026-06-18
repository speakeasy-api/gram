package types

// Field identifies a logical value that handlers can resolve from a
// provider-native payload without knowing that provider's payload shape.
type Field string

const (
	FieldEventType Field = "event.type"

	FieldScannableText   Field = "scan.text"
	FieldScanMessageType Field = "scan.message_type"

	FieldToolName Field = "tool.name"
)
