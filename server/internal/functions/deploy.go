package functions

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type RunnerImageRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID

	Runtime Runtime
}

type RunnerDeployRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID

	Runtime        Runtime
	RuntimeVersion string
	EnvSecrets     string
	Assets         []RunnerAssetMount
}

type RunnerUpdateRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID

	Runtime        Runtime
	RuntimeVersion string
	EnvSecrets     string
	Assets         []RunnerAssetMount
}

type RunnerDestroyRequest struct {
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	FunctionsID  uuid.UUID
}

type RunnerAssetMount struct {
	AssetID uuid.UUID

	GuestPath string
	Mode      string
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
