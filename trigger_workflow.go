package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background"
	"go.temporal.io/sdk/client"
)

func main() {
	ctx := context.Background()

	// Connect to Temporal
	temporalClient, err := client.Dial(client.Options{
		HostPort:  "localhost:7233",
		Namespace: "default",
	})
	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer temporalClient.Close()

	// Create the metrics client
	metricsClient := &background.PlatformUsageMetricsClient{
		Temporal: temporalClient,
	}

	fmt.Println("🚀 Starting platform usage metrics workflow...")

	// Start the workflow
	workflowRun, err := metricsClient.StartCollectPlatformUsageMetrics(ctx)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	fmt.Printf("✅ Workflow started successfully!\n")
	fmt.Printf("   Workflow ID: %s\n", workflowRun.GetID())
	fmt.Printf("   Run ID: %s\n", workflowRun.GetRunID())
	fmt.Printf("   Temporal Web UI: http://localhost:8233\n")
	
	// Wait a moment to see if it starts properly
	fmt.Println("\n⏳ Checking workflow status...")
	time.Sleep(2 * time.Second)
	
	// Check if workflow is running
	workflowInfo, err := temporalClient.DescribeWorkflowExecution(ctx, workflowRun.GetID(), workflowRun.GetRunID())
	if err != nil {
		fmt.Printf("❌ Could not get workflow status: %v\n", err)
	} else {
		fmt.Printf("📊 Workflow Status: %s\n", workflowInfo.WorkflowExecutionInfo.Status)
		if workflowInfo.WorkflowExecutionInfo.CloseTime != nil {
			fmt.Printf("📅 Close Time: %s\n", workflowInfo.WorkflowExecutionInfo.CloseTime)
		}
	}
}