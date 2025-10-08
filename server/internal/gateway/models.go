package gateway

import (
	"errors"
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

// HTTPTool describes how to translate a tool call into an HTTP request to be
// proxied to some downstream server.
type HTTPTool struct {
	ID             string `json:"id" yaml:"id"`
	DeploymentID   string `json:"deployment_id" yaml:"deployment_id"`
	ProjectID      string `json:"project_id" yaml:"project_id"`
	OrganizationID string `json:"organization_id" yaml:"organization_id"`
	Name           string `json:"name" yaml:"name"`

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

	ResponseFilter *ResponseFilter `json:"response_filter" yaml:"response_filter"`
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

// FunctionTool describes a serverless function that can be invoked as a tool.
type FunctionTool struct {
	ID             string `json:"id" yaml:"id"`
	DeploymentID   string `json:"deployment_id" yaml:"deployment_id"`
	ProjectID      string `json:"project_id" yaml:"project_id"`
	OrganizationID string `json:"organization_id" yaml:"organization_id"`
	FunctionID     string `json:"function_id" yaml:"function_id"`
	Name           string `json:"name" yaml:"name"`
	Runtime        string `json:"runtime" yaml:"runtime"`
	InputSchema    []byte `json:"input_schema" yaml:"input_schema"`
	Variables      []byte `json:"variables" yaml:"variables"`
}

type ToolKind string

const (
	ToolKindHTTP     ToolKind = "http"
	ToolKindFunction ToolKind = "function"
)

// Tool is a polymorphic type that can represent either an HTTPTool or a FunctionTool.
// Use NewHTTPTool or NewFunctionTool to create instances.
type Tool struct {
	kind         ToolKind
	HTTPTool     *HTTPTool
	FunctionTool *FunctionTool
}

// Kind returns the tool kind.
func (t *Tool) Kind() ToolKind {
	return t.kind
}

// NewHTTPTool creates a new Tool wrapping an HTTPTool.
func NewHTTPTool(httpTool *HTTPTool) *Tool {
	return &Tool{
		kind:         ToolKindHTTP,
		HTTPTool:     httpTool,
		FunctionTool: nil,
	}
}

// NewFunctionTool creates a new Tool wrapping a FunctionTool.
func NewFunctionTool(functionTool *FunctionTool) *Tool {
	return &Tool{
		kind:         ToolKindFunction,
		HTTPTool:     nil,
		FunctionTool: functionTool,
	}
}

// IsHTTP returns true if this tool is an HTTP tool.
func (t *Tool) IsHTTP() bool {
	return t.HTTPTool != nil && t.kind == ToolKindHTTP
}

// IsFunction returns true if this tool is a function tool.
func (t *Tool) IsFunction() bool {
	return t.FunctionTool != nil && t.kind == ToolKindFunction
}
