//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

const (
	maxSupportMatrixEntries = 200
	maxSupportMatrixFacts   = 2000
	maxSupportMatrixText    = 512
)

var errSupportMatrixUnavailable = errors.New("support matrix is unavailable")

// SupportMatrixReader is the admin service's read-only global support catalogue contract.
type SupportMatrixReader interface {
	GetSupportMatrix(context.Context, *gen.GetSupportMatrixPayload) (*gen.SupportMatrix, error)
}

type GetSupportMatrixOutput struct {
	Methods      []SupportMatrixMethod     `json:"methods"`
	Products     []SupportMatrixProduct    `json:"products"`
	Capabilities []SupportMatrixCapability `json:"capabilities"`
	Mappings     []SupportMatrixMapping    `json:"mappings"`
	References   []SupportMatrixReference  `json:"references"`
}

type SupportMatrixMethod struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Vendor string              `json:"vendor"`
	Facts  []SupportMatrixFact `json:"facts"`
}

type SupportMatrixProduct struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Vendor  string `json:"vendor"`
	Family  string `json:"family"`
	Surface string `json:"surface"`
}

type SupportMatrixCapability struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

type SupportMatrixFact struct {
	CapabilityID string `json:"capability_id"`
	Status       string `json:"status"`
}

type SupportMatrixMapping struct {
	MethodID      string              `json:"method_id"`
	ProductID     string              `json:"product_id"`
	Applicability string              `json:"applicability"`
	Facts         []SupportMatrixFact `json:"facts"`
}

type SupportMatrixReference struct {
	MethodID string              `json:"method_id"`
	Facts    []SupportMatrixFact `json:"facts"`
}

func registerSupportMatrixTools(server *mcp.Server, reader SupportMatrixReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_support_matrix",
		Title:       "Get Global Support Matrix Facts",
		Description: "Read the global support catalogue: methods, products, capabilities, and declared applicability and status facts. Notes, conditions, plan text, verification flags, and revisions are intentionally omitted.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GetSupportMatrixOutput, error) {
		output := GetSupportMatrixOutput{
			Methods: []SupportMatrixMethod{}, Products: []SupportMatrixProduct{},
			Capabilities: []SupportMatrixCapability{}, Mappings: []SupportMatrixMapping{}, References: []SupportMatrixReference{},
		}
		if reader == nil || !verifiedStaff(ctx) {
			return nil, output, errSupportMatrixUnavailable
		}
		matrix, err := reader.GetSupportMatrix(ctx, &gen.GetSupportMatrixPayload{})
		if err != nil || matrix == nil || matrix.Draft == nil || len(matrix.Methods) > maxSupportMatrixEntries || len(matrix.Products) > maxSupportMatrixEntries || len(matrix.Capabilities) > maxSupportMatrixEntries || len(matrix.Draft.Mappings) > maxSupportMatrixFacts || len(matrix.Draft.References) > maxSupportMatrixEntries {
			return nil, output, errSupportMatrixUnavailable
		}

		factCount := 0
		for _, method := range matrix.Methods {
			if method == nil || !validSupportMatrixText(method.ID) || !validSupportMatrixText(method.Name) || !validSupportMatrixText(method.Vendor) {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			facts, valid := projectSupportMatrixFacts(method.Facts)
			factCount += len(facts)
			if !valid || factCount > maxSupportMatrixFacts {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Methods = append(output.Methods, SupportMatrixMethod{ID: method.ID, Name: method.Name, Vendor: method.Vendor, Facts: facts})
		}
		for _, product := range matrix.Products {
			if product == nil || !validSupportMatrixText(product.ID) || !validSupportMatrixText(product.Name) || !validSupportMatrixText(product.Vendor) || !validSupportMatrixText(product.Family) || !validSupportMatrixText(product.Surface) {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Products = append(output.Products, SupportMatrixProduct{ID: product.ID, Name: product.Name, Vendor: product.Vendor, Family: product.Family, Surface: product.Surface})
		}
		for _, capability := range matrix.Capabilities {
			if capability == nil || !validSupportMatrixText(capability.ID) || !validSupportMatrixText(capability.Name) || !validSupportMatrixText(capability.Group) {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Capabilities = append(output.Capabilities, SupportMatrixCapability{ID: capability.ID, Name: capability.Name, Group: capability.Group})
		}

		for _, key := range slices.Sorted(maps.Keys(matrix.Draft.Mappings)) {
			mapping := matrix.Draft.Mappings[key]
			methodID, productID, ok := splitSupportMatrixMappingKey(key)
			if !ok || mapping == nil || (mapping.Applicability != "unknown" && mapping.Applicability != "applicable" && mapping.Applicability != "na") {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			facts, valid := projectSupportMatrixFacts(mapping.Facts)
			factCount += len(facts)
			if !valid || factCount > maxSupportMatrixFacts {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Mappings = append(output.Mappings, SupportMatrixMapping{MethodID: methodID, ProductID: productID, Applicability: mapping.Applicability, Facts: facts})
		}
		for _, methodID := range slices.Sorted(maps.Keys(matrix.Draft.References)) {
			factsByCapability := matrix.Draft.References[methodID]
			if !validSupportMatrixText(methodID) {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			facts, valid := projectSupportMatrixFacts(factsByCapability)
			factCount += len(facts)
			if !valid || factCount > maxSupportMatrixFacts {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.References = append(output.References, SupportMatrixReference{MethodID: methodID, Facts: facts})
		}
		return nil, output, nil
	})
}

func projectSupportMatrixFacts(input map[string]*gen.SupportFact) ([]SupportMatrixFact, bool) {
	if len(input) > maxSupportMatrixFacts {
		return nil, false
	}
	facts := make([]SupportMatrixFact, 0, len(input))
	for _, capabilityID := range slices.Sorted(maps.Keys(input)) {
		fact := input[capabilityID]
		if !validSupportMatrixText(capabilityID) || fact == nil || !validSupportMatrixStatus(fact.Status) {
			return nil, false
		}
		facts = append(facts, SupportMatrixFact{CapabilityID: capabilityID, Status: fact.Status})
	}
	return facts, true
}

func validSupportMatrixText(value string) bool {
	return utf8.ValidString(value) && len(value) <= maxSupportMatrixText
}

func validSupportMatrixStatus(status string) bool {
	switch status {
	case "supported", "partial", "unimplemented", "impossible", "na", "unknown":
		return true
	default:
		return false
	}
}

func splitSupportMatrixMappingKey(key string) (string, string, bool) {
	for index := 0; index < len(key); index++ {
		if key[index] == '/' {
			methodID, productID := key[:index], key[index+1:]
			return methodID, productID, validSupportMatrixText(methodID) && validSupportMatrixText(productID) && productID != ""
		}
	}
	return "", "", false
}
