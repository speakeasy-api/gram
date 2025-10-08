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
}

type ToolCaller interface {
	CallTool(context.Context, RunnerToolCallRequest) (*http.Response, error)
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

	Runtime Runtime
	Assets  []RunnerAssetMount

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

type RunnerAssetMount struct {
	AssetURL *url.URL

	GuestPath string
	// Mode is a string representation of file mode, e.g. 0444
	Mode uint32
}

type RunnerToolCallRequest struct {
	InvocationID uuid.UUID

	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID

	URN         urn.Tool
	Name        string
	Input       json.RawMessage
	Environment map[string]string
}
