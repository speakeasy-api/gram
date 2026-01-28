package gateway

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type FilterType string

const (
	FilterTypeNone FilterType = "none"
	FilterTypeJQ   FilterType = "jq"
)

func NewFilterType(s string) (FilterType, error) {
	switch s {
	case "none", "":
		return FilterTypeNone, nil
	case "jq":
		return FilterTypeJQ, nil
	default:
		return FilterTypeNone, errors.New("invalid filter type: " + s)
	}
}

type ToolDescriptor struct {
	ID               string   `json:"id" yaml:"id"`
	Name             string   `json:"name" yaml:"name"`
	Description      *string  `json:"description" yaml:"description"`
	DeploymentID     string   `json:"deployment_id" yaml:"deployment_id"`
	ProjectID        string   `json:"project_id" yaml:"project_id"`
	ProjectSlug      string   `json:"project_slug" yaml:"project_slug"`
	OrganizationID   string   `json:"organization_id" yaml:"organization_id"`
	OrganizationSlug string   `json:"organization_slug" yaml:"organization_slug"`
	URN              urn.Tool `json:"urn" yaml:"urn"`
}

type ResourceDescriptor struct {
	ID               string       `json:"id" yaml:"id"`
	Name             string       `json:"name" yaml:"name"`
	DeploymentID     string       `json:"deployment_id" yaml:"deployment_id"`
	ProjectID        string       `json:"project_id" yaml:"project_id"`
	ProjectSlug      string       `json:"project_slug" yaml:"project_slug"`
	OrganizationID   string       `json:"organization_id" yaml:"organization_id"`
	OrganizationSlug string       `json:"organization_slug" yaml:"organization_slug"`
	URN              urn.Resource `json:"urn" yaml:"urn"`
	URI              string       `json:"uri" yaml:"uri"`
}

// HTTPToolCallPlan describes how to translate a tool call into an HTTP request to be
// proxied to some downstream server.
type HTTPToolCallPlan struct {
	DefaultServerUrl   NullString                `json:"default_server_url" yaml:"default_server_url"`
	ServerEnvVar       string                    `json:"server_env_var" yaml:"server_env_var"`
	Method             string                    `json:"method" yaml:"method"`
	Path               string                    `json:"path" yaml:"path"`
	Schema             []byte                    `json:"schema" yaml:"schema"`
	PathParams         map[string]*HTTPParameter `json:"path_params" yaml:"path_params"`
	QueryParams        map[string]*HTTPParameter `json:"query_params" yaml:"query_params"`
	HeaderParams       map[string]*HTTPParameter `json:"header_params" yaml:"header_params"`
	RequestContentType NullString                `json:"request_content_type" yaml:"request_content_type"`
	Security           []*HTTPToolSecurity       `json:"security" yaml:"security"`
	SecurityScopes     map[string][]string       `json:"security_scopes" yaml:"security_scopes"`
	ResponseFilter     *ResponseFilter           `json:"response_filter" yaml:"response_filter"`
}

// HTTPParameter holds the settings for encoding a parameter into an HTTP
// request.
type HTTPParameter struct {
	// Name is the name of the parameter as it should appear in the request.
	Name string `json:"name" yaml:"name"`
	// Style defines how the parameter encoding to use.
	Style string `json:"style" yaml:"style"`
	// Explode indicates whether the parameter should be exploded when it is an
	// array or object.
	Explode *bool `json:"explode" yaml:"explode"`
	// AllowEmptyValue indicates whether the parameter should appear in the
	// request even when it is empty.
	AllowEmptyValue bool `json:"allow_empty_value" yaml:"allow_empty_value"`
}

// HTTPToolSecurity describes the security requirements for a given HTTP endpoint.
type HTTPToolSecurity struct {
	ID           string     `json:"id" yaml:"id"`
	Key          string     `json:"key" yaml:"key"`
	Type         NullString `json:"type" yaml:"type"`
	Scheme       NullString `json:"scheme" yaml:"scheme"`
	Name         NullString `json:"name" yaml:"name"`
	Placement    NullString `json:"placement" yaml:"placement"`
	OAuthTypes   []string   `json:"oauth_types" yaml:"oauth_types"`
	OAuthFlows   []byte     `json:"oauth_flows" yaml:"oauth_flows"`
	EnvVariables []string   `json:"env_vars" yaml:"env_vars"`
}

// ResponseFilter describe an API response schema that can be filtered with an
// expression (jq and similar) provided at tool call time.
type ResponseFilter struct {
	Type         FilterType `json:"type" yaml:"type"`
	Schema       []byte     `json:"schema" yaml:"schema"`
	StatusCodes  []string   `json:"status_codes" yaml:"status_codes"`
	ContentTypes []string   `json:"content_types" yaml:"content_types"`
}

var DisableResponseFiltering = &ResponseFilter{
	Type:         FilterTypeNone,
	Schema:       []byte{},
	StatusCodes:  []string{},
	ContentTypes: []string{},
}

// FunctionToolCallPlan describes a serverless function that can be invoked as a tool.
type FunctionToolCallPlan struct {
	FunctionID        string                                            `json:"function_id" yaml:"function_id"`
	FunctionsAccessID string                                            `json:"functions_access_id" yaml:"functions_access_id"`
	Runtime           string                                            `json:"runtime" yaml:"runtime"`
	InputSchema       []byte                                            `json:"input_schema" yaml:"input_schema"`
	Variables         map[string]*functions.ManifestVariableAttributeV0 `json:"variables" yaml:"variables"`
	AuthInput         *functions.ManifestAuthInputAttributeV0           `json:"auth_input,omitempty" yaml:"auth_input,omitempty"`
}

type PromptToolCallPlan struct {
	TemplateID string `json:"template_id" yaml:"template_id"`
	Prompt     string `json:"prompt" yaml:"prompt"`
	Engine     string `json:"engine" yaml:"engine"`
	Kind       string `json:"kind" yaml:"kind"`
}

// ExternalMCPToolCallPlan is an alias for externalmcp.ToolCallPlan.
// Kept for backwards compatibility with existing code.
type ExternalMCPToolCallPlan = externalmcp.ToolCallPlan

// ExternalMCPHeaderDef is an alias for externalmcp.HeaderDefinition.
// Kept for backwards compatibility with existing code.
type ExternalMCPHeaderDef = externalmcp.HeaderDefinition

type ResourceFunctionCallPlan struct {
	FunctionID        string   `json:"function_id" yaml:"function_id"`
	FunctionsAccessID string   `json:"functions_access_id" yaml:"functions_access_id"`
	Runtime           string   `json:"runtime" yaml:"runtime"`
	URI               string   `json:"uri" yaml:"uri"`
	MimeType          string   `json:"mime_type" yaml:"mime_type"`
	Variables         []string `json:"variables" yaml:"variables"`
}

type ToolKind string

const (
	ToolKindHTTP        ToolKind = "http"
	ToolKindFunction    ToolKind = "function"
	ToolKindPrompt      ToolKind = "prompt"
	ToolKindExternalMCP ToolKind = "external_mcp"
)

type ResourceKind string

const (
	ResourceKindFunction ResourceKind = "function"
)

// ToolCallPlan is a polymorphic type that can represent either an HTTPTool or a FunctionTool.
// Use NewHTTPTool or NewFunctionTool to create instances.
type ToolCallPlan struct {
	Kind        ToolKind
	BillingType billing.ToolCallType
	Descriptor  *ToolDescriptor

	HTTP        *HTTPToolCallPlan
	Function    *FunctionToolCallPlan
	Prompt      *PromptToolCallPlan
	ExternalMCP *ExternalMCPToolCallPlan
}

// NewHTTPToolCallPlan creates a new Tool wrapping an HTTPTool.
func NewHTTPToolCallPlan(tool *ToolDescriptor, plan *HTTPToolCallPlan) *ToolCallPlan {
	return &ToolCallPlan{
		Kind:        ToolKindHTTP,
		BillingType: billing.ToolCallTypeHTTP,
		Descriptor:  tool,
		HTTP:        plan,
		Function:    nil,
		Prompt:      nil,
		ExternalMCP: nil,
	}
}

// NewFunctionToolCallPlan creates a new Tool wrapping a FunctionTool.
func NewFunctionToolCallPlan(tool *ToolDescriptor, plan *FunctionToolCallPlan) *ToolCallPlan {
	return &ToolCallPlan{
		Kind:        ToolKindFunction,
		BillingType: billing.ToolCallTypeFunction,
		Descriptor:  tool,
		HTTP:        nil,
		Function:    plan,
		Prompt:      nil,
		ExternalMCP: nil,
	}
}

// NewPromptToolCallPlan creates a new Tool wrapping a PromptTool.
func NewPromptToolCallPlan(tool *ToolDescriptor, plan *PromptToolCallPlan) *ToolCallPlan {
	return &ToolCallPlan{
		Kind:        ToolKindPrompt,
		BillingType: billing.ToolCallTypeHigherOrder,
		Descriptor:  tool,
		HTTP:        nil,
		Function:    nil,
		Prompt:      plan,
		ExternalMCP: nil,
	}
}

// NewExternalMCPToolCallPlan creates a new Tool wrapping an ExternalMCPTool.
func NewExternalMCPToolCallPlan(tool *ToolDescriptor, plan *ExternalMCPToolCallPlan) *ToolCallPlan {
	return &ToolCallPlan{
		Kind:        ToolKindExternalMCP,
		BillingType: billing.ToolCallTypeExternalMCP,
		Descriptor:  tool,
		HTTP:        nil,
		Function:    nil,
		Prompt:      nil,
		ExternalMCP: plan,
	}
}

type ResourceCallPlan struct {
	Kind        ResourceKind
	BillingType billing.ToolCallType
	Descriptor  *ResourceDescriptor

	Function *ResourceFunctionCallPlan
}

func NewResourceFunctionCallPlan(resource *ResourceDescriptor, plan *ResourceFunctionCallPlan) *ResourceCallPlan {
	return &ResourceCallPlan{
		Kind:        ResourceKindFunction,
		BillingType: billing.ToolCallTypeFunction,
		Descriptor:  resource,
		Function:    plan,
	}
}
