package testenv

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/durationpb"
)

// newTemporalClientFactory creates a factory function that returns a Temporal client
// connected to the test Temporal server. Connection details are read from
// TEST_TEMPORAL_ADDRESS environment variable.
//
// Each test gets its own unique namespace for isolation, matching the original
// testcontainers behavior where each test had its own Temporal dev server.
func newTemporalClientFactory() (func(t *testing.T) client.Client, error) {
	address := os.Getenv("TEST_TEMPORAL_ADDRESS")

	if address == "" {
		return nil, fmt.Errorf("TEST_TEMPORAL_ADDRESS environment variable must be set")
	}

	return func(t *testing.T) client.Client {
		t.Helper()

		logger := NewLogger(t)

		// Create a unique namespace for this test (matching original testcontainers behavior)
		namespace := fmt.Sprintf("test_%s", nextRandom())

		// Register the namespace with the Temporal server
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		// Create a client without namespace first to register the namespace
		adminClient, err := client.NewNamespaceClient(client.Options{
			HostPort: address,
			Logger:   logger,
		})
		if err != nil {
			t.Fatalf("failed to create temporal namespace client: %v", err)
		}

		err = adminClient.Register(ctx, &workflowservice.RegisterNamespaceRequest{
			Namespace:                        namespace,
			Description:                      "Test namespace",
			OwnerEmail:                       "test@example.com",
			WorkflowExecutionRetentionPeriod: durationpb.New(24 * time.Hour),
			Clusters:                         nil,
			ActiveClusterName:                "",
			Data:                             nil,
			SecurityToken:                    "",
			IsGlobalNamespace:                false,
			HistoryArchivalState:             0,
			HistoryArchivalUri:               "",
			VisibilityArchivalState:          0,
			VisibilityArchivalUri:            "",
		})
		if err != nil {
			// Namespace might already exist, which is fine
			t.Logf("namespace registration (may already exist): %v", err)
		}
		adminClient.Close()

		// Now create the actual client with the namespace
		c, err := client.Dial(client.Options{
			HostPort:  address,
			Namespace: namespace,
			Logger:    logger,
		})
		if err != nil {
			t.Fatalf("failed to create temporal client: %v", err)
		}

		t.Cleanup(func() {
			c.Close()
		})

		return c
	}, nil
}
