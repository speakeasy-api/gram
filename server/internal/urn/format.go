package urn

// delimiter is the character that separates the parts of a URN.
const delimiter = ":"

// maxSegmentLength is the maximum length of each segment in URNs (kind, source,
// name). Application code can further enforce shorter lengths. The purpose of
// this is to place an upper bound on parsing logic.
const maxSegmentLength = 128

type ToolKind string

const (
	ToolKindFunction    ToolKind = "function"
	ToolKindHTTP        ToolKind = "http"
	ToolKindPrompt      ToolKind = "prompt"
	ToolKindExternalMCP ToolKind = "externalmcp"
)

var toolKinds = map[ToolKind]struct{}{
	ToolKindFunction:    {},
	ToolKindHTTP:        {},
	ToolKindPrompt:      {},
	ToolKindExternalMCP: {},
}

type ResourceKind string

const (
	ResourceKindFunction ResourceKind = "function"
)

var resourceKinds = map[ResourceKind]struct{}{
	ResourceKindFunction: {},
}

type AssetKind string

const (
	AssetKindImage    AssetKind = "image"
	AssetKindFunction AssetKind = "function"
	AssetKindOpenAPI  AssetKind = "openapi"
)

var assetKinds = map[AssetKind]struct{}{
	AssetKindImage:    {},
	AssetKindFunction: {},
	AssetKindOpenAPI:  {},
}

type VariationKind string

const (
	VariationKindGlobal  VariationKind = "global"
	VariationKindToolset VariationKind = "toolset"
)

var variationKinds = map[VariationKind]struct{}{
	VariationKindGlobal:  {},
	VariationKindToolset: {},
}
