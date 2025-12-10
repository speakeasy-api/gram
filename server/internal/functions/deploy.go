package functions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type RunnerVersion string

type Orchestrator interface {
	Deployer
	ToolCaller
}

type Deployer interface {
	Deploy(context.Context, RunnerDeployRequest) (*RunnerDeployResult, error)
	Reap(context.Context, ReapRequest) error
}

type ToolCaller interface {
	ToolCall(context.Context, RunnerToolCallRequest) (*http.Request, error)
	ReadResource(context.Context, RunnerResourceReadRequest) (*http.Request, error)
}

type RunnerImageRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID

	Runtime Runtime
}

type RunnerDeployRequest struct {
	Version RunnerVersion

	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionID   uuid.UUID
	AccessID     uuid.UUID

	Runtime Runtime
	Assets  []RunnerAsset

	BearerSecret string
}

type RunnerDeployResult struct {
	URN       urn.FunctionRunner
	PublicURL *url.URL
	Version   RunnerVersion
	Provider  string
	Region    string
	Scale     int
}

type RunnerDestroyRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID
}

type RunnerAsset struct {
	AssetID  uuid.UUID
	AssetURL *url.URL

	GuestPath string
	// Mode is a string representation of file mode, e.g. 0444
	Mode uint32

	SHA256Sum     string
	ContentLength int64
	ContentType   string
}

type RunnerBaseRequest struct {
	InvocationID uuid.UUID

	OrganizationID    string
	OrganizationSlug  string
	ProjectID         uuid.UUID
	ProjectSlug       string
	DeploymentID      uuid.UUID
	FunctionsID       uuid.UUID
	FunctionsAccessID uuid.UUID

	Input       json.RawMessage
	Environment map[string]string
}

type RunnerToolCallRequest struct {
	RunnerBaseRequest

	ToolURN  urn.Tool
	ToolName string
}

type RunnerResourceReadRequest struct {
	RunnerBaseRequest

	ResourceURN  urn.Resource
	ResourceURI  string
	ResourceName string
}

type ReapRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionID   uuid.UUID
}
