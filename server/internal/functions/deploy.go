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
	ToolCall(context.Context, RunnerToolCallRequest) (*http.Request, error)
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
	AssetURL *url.URL

	GuestPath string
	// Mode is a string representation of file mode, e.g. 0444
	Mode uint32

	SHA256Sum string
}

type RunnerToolCallRequest struct {
	InvocationID uuid.UUID

	OrganizationID    string
	OrganizationSlug  string
	ProjectID         uuid.UUID
	ProjectSlug       string
	DeploymentID      uuid.UUID
	FunctionsID       uuid.UUID
	FunctionsAccessID uuid.UUID

	ToolURN         urn.Tool
	ToolName        string
	ToolInput       json.RawMessage
	ToolEnvironment map[string]string
}
