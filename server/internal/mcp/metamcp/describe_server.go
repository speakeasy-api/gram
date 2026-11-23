package metamcp

// DescribedTool is one member tool as reported by describe_server: the
// qualified name and description, deliberately without the input schema —
// that is describe_tools' job, keeping the catalog listing small.
type DescribedTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// DescribeServerResult is the structuredContent payload of a describe_server
// call: one member's identity, instructions when available, and tool catalog.
type DescribeServerResult struct {
	Server       ListedServer    `json:"server"`
	Instructions string          `json:"instructions,omitempty"`
	Tools        []DescribedTool `json:"tools"`
}
