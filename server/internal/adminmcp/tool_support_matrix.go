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

// SupportMatrixReader is the admin service's read of the support matrix the
// server was built with.
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

// SupportMatrixAccounts says which account types can use the method on the product.
type SupportMatrixAccounts struct {
	Personal   string `json:"personal"`
	Team       string `json:"team"`
	Enterprise string `json:"enterprise"`
}

type SupportMatrixMapping struct {
	MethodID      string                `json:"method_id"`
	ProductID     string                `json:"product_id"`
	Applicability string                `json:"applicability"`
	Accounts      SupportMatrixAccounts `json:"accounts"`
	// Facts are the cells: one per capability when the method applies to the
	// product, none otherwise.
	Facts []SupportMatrixFact `json:"facts"`
}

type SupportMatrixReference struct {
	MethodID string              `json:"method_id"`
	Facts    []SupportMatrixFact `json:"facts"`
}

func registerSupportMatrixTools(server *mcp.Server, reader SupportMatrixReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_support_matrix",
		Title:       "Get Global Support Matrix Facts",
		Description: "Read the support matrix the server was built with: methods, products, capabilities, each method's claims per capability, and per product whether the method applies, which account types can use it, and one status per capability when it applies. Notes, plan text, operating systems, verification flags and the revision are intentionally omitted. The matrix is code, changed by pull request.",
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
		if err != nil || matrix == nil || len(matrix.Methods) > maxSupportMatrixEntries || len(matrix.Platforms) > maxSupportMatrixEntries || len(matrix.Capabilities) > maxSupportMatrixEntries {
			return nil, output, errSupportMatrixUnavailable
		}

		for _, capability := range matrix.Capabilities {
			if capability == nil || !validSupportMatrixText(capability.ID) || !validSupportMatrixText(capability.Name) || !validSupportMatrixText(capability.Group) {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Capabilities = append(output.Capabilities, SupportMatrixCapability{ID: capability.ID, Name: capability.Name, Group: capability.Group})
		}
		for _, product := range matrix.Platforms {
			if product == nil || !validSupportMatrixText(product.ID) || !validSupportMatrixText(product.Name) || !validSupportMatrixText(product.Vendor) || !validSupportMatrixText(product.Family) || !validSupportMatrixText(product.Surface) {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Products = append(output.Products, SupportMatrixProduct{ID: product.ID, Name: product.Name, Vendor: product.Vendor, Family: product.Family, Surface: product.Surface})
		}
		factCount := 0
		for _, method := range matrix.Methods {
			if method == nil || !validSupportMatrixText(method.ID) || !validSupportMatrixText(method.Name) || !validSupportMatrixText(method.Vendor) || len(method.Platforms) > maxSupportMatrixEntries {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			claims, valid := projectSupportMatrixFacts(method.Claims)
			// Claims are reported twice, with the method and as its reference.
			factCount += 2 * len(claims)
			if !valid || factCount > maxSupportMatrixFacts {
				return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
			}
			output.Methods = append(output.Methods, SupportMatrixMethod{ID: method.ID, Name: method.Name, Vendor: method.Vendor, Facts: claims})
			output.References = append(output.References, SupportMatrixReference{MethodID: method.ID, Facts: claims})
			for _, support := range method.Platforms {
				if support == nil || !validSupportMatrixText(support.Platform) || support.Platform == "" || !validSupportMatrixApplicability(support.Applicability) || support.Accounts == nil {
					return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
				}
				for _, eligibility := range []string{support.Accounts.Personal, support.Accounts.Team, support.Accounts.Enterprise} {
					if !validSupportMatrixEligibility(eligibility) {
						return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
					}
				}
				cells, valid := projectSupportMatrixFacts(support.Cells)
				factCount += len(cells)
				if !valid || factCount > maxSupportMatrixFacts {
					return nil, GetSupportMatrixOutput{}, errSupportMatrixUnavailable
				}
				output.Mappings = append(output.Mappings, SupportMatrixMapping{
					MethodID: method.ID, ProductID: support.Platform, Applicability: support.Applicability,
					Accounts: SupportMatrixAccounts{Personal: support.Accounts.Personal, Team: support.Accounts.Team, Enterprise: support.Accounts.Enterprise},
					Facts:    cells,
				})
			}
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

func validSupportMatrixApplicability(value string) bool {
	switch value {
	case "applicable", "na", "unknown":
		return true
	default:
		return false
	}
}

func validSupportMatrixEligibility(value string) bool {
	switch value {
	case "supported", "unsupported", "unknown":
		return true
	default:
		return false
	}
}
