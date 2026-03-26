package testenv

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/durationpb"

	servertemporal "github.com/speakeasy-api/gram/server/internal/temporal"
)

func NewTemporalDevServer(ctx context.Context) (*testsuite.DevServer, error) {
	var stdout io.Writer
	var stderr io.Writer
	if !isTestingVerbose() {
		stdout = io.Discard
		stderr = io.Discard
	}

	var devserver *testsuite.DevServer
	var err error
	logger := NewLogger(nil)

	for range 5 {
		devserver, err = testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
			LogLevel:     "error",
			ExistingPath: "temporal",
			ClientOptions: &client.Options{
				Namespace: "default",
				Logger:    logger,
			},
			Stdout: stdout,
			Stderr: stderr,
		})
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("start temporal dev server: %w", err)
	}

	return devserver, nil
}

func NewTemporalEnvironment(t *testing.T, devserver *testsuite.DevServer) (*servertemporal.Environment, error) {
	t.Helper()

	namespace := fmt.Sprintf("test_%s", nextRandom())
	queue := fmt.Sprintf("main_%s", nextRandom())

	request := new(workflowservice.RegisterNamespaceRequest)
	request.Namespace = namespace
	request.WorkflowExecutionRetentionPeriod = durationpb.New(24 * time.Hour)

	_, err := devserver.Client().WorkflowService().RegisterNamespace(t.Context(), request)
	if err != nil {
		return nil, fmt.Errorf("register temporal namespace: %w", err)
	}

	clientOptions := client.Options{}
	clientOptions.HostPort = devserver.FrontendHostPort()
	clientOptions.Namespace = namespace
	clientOptions.Logger = NewLogger(t)

	temporalClient, err := client.DialContext(t.Context(), clientOptions)
	if err != nil {
		return nil, fmt.Errorf("dial temporal client: %w", err)
	}

	t.Cleanup(func() {
		temporalClient.Close()
	})

	return servertemporal.NewEnvironment(temporalClient, servertemporal.NamespaceName(namespace), servertemporal.TaskQueueName(queue)), nil
}
