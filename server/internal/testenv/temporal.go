package testenv

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"testing"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/speakeasy-api/gram/server/internal/conv"
	servertemporal "github.com/speakeasy-api/gram/server/internal/temporal"
)

func NewTemporalDevServer(ctx context.Context) (*testsuite.DevServer, string, error) {
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
		return nil, "", fmt.Errorf("start temporal dev server: %w", err)
	}

	uri := url.URL{
		Scheme: "temporal",
		Host:   devserver.FrontendHostPort(),
		Path:   "default",
	}

	return devserver, uri.String(), nil
}

func NewTemporalEnvironment(t *testing.T, uri string) (*servertemporal.Environment, error) {
	t.Helper()

	namespace := fmt.Sprintf("test_%s", nextRandom())
	queue := fmt.Sprintf("main_%s", nextRandom())

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse temporal URI: %w", err)
	}

	logger := NewLogger(t)

	request := new(workflowservice.RegisterNamespaceRequest)
	request.Namespace = namespace
	request.WorkflowExecutionRetentionPeriod = durationpb.New(24 * time.Hour)

	defaultNSOptions := client.Options{
		HostPort:  parsed.Host,
		Namespace: conv.Default(parsed.Path, "/default")[1:], // skip leading slash
		Logger:    logger,
	}
	temporalClient, err := client.DialContext(t.Context(), defaultNSOptions)
	if err != nil {
		return nil, fmt.Errorf("dial temporal client to default namespace: %w", err)
	}

	_, err = temporalClient.WorkflowService().RegisterNamespace(t.Context(), request)
	if err != nil {
		return nil, fmt.Errorf("register temporal namespace: %w", err)
	}

	temporalClient.Close()

	clientOptions := client.Options{}
	clientOptions.HostPort = parsed.Host
	clientOptions.Namespace = namespace
	clientOptions.Logger = logger

	temporalClient, err = client.DialContext(t.Context(), clientOptions)
	if err != nil {
		return nil, fmt.Errorf("dial temporal client: %w", err)
	}

	t.Cleanup(func() {
		temporalClient.Close()
	})

	return servertemporal.NewEnvironment(temporalClient, servertemporal.NamespaceName(namespace), servertemporal.TaskQueueName(queue)), nil
}
