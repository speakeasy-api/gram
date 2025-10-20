package functions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type LocalRunner struct {
	codeRoot       *os.Root
	toolcallClient *http.Client
}

var _ Orchestrator = (*LocalRunner)(nil)

func NewLocalRunner(codeRoot *os.Root) *LocalRunner {
	return &LocalRunner{
		codeRoot:       codeRoot,
		toolcallClient: retryablehttp.NewClient().StandardClient(),
	}
}

func (l *LocalRunner) ToolCall(ctx context.Context, call RunnerToolCallRequest) (*http.Request, error) {
	return nil, oops.Permanent(errors.New("not implemented"))
}

func (l *LocalRunner) ReadResource(ctx context.Context, readReq RunnerResourceReadRequest) (*http.Request, error) {
	return nil, oops.Permanent(errors.New("not implemented"))
}

func (l *LocalRunner) Deploy(ctx context.Context, req RunnerDeployRequest) (*RunnerDeployResult, error) {
	name := fmt.Sprintf("dev-%s", req.FunctionID.String())

	return &RunnerDeployResult{
		URN:       urn.NewFunctionRunner(urn.FunctionRunnerKindLocal, "local", name),
		PublicURL: nil,
		Version:   "dev",
		Provider:  "local",
		Region:    "local",
		Scale:     1,
	}, nil
}
