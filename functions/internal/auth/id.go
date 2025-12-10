package auth

import (
	"github.com/speakeasy-api/gram/functions/internal/svc"
)

type RunnerIdentity struct {
	AuthSecret   svc.Secret[[]byte] `json:"-"`
	Version      string             `json:"version"`
	ProjectID    string             `json:"project_id"`
	DeploymentID string             `json:"deployment_id"`
	FunctionID   string             `json:"function_id"`
}
