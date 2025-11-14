import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";
import { Connection, Client } from "@temporalio/client";

const gram = new Gram({
  envSchema: {
    TEMPORAL_API_KEY: z.string(),
    TEMPORAL_GRPC_ENDPOINT: z.string(),
    TEMPORAL_NAMESPACE: z.string(),
  },
}).tool({
  name: "query_workflows",
  description: "Query Temporal workflow executions with possible filters on workflow type or by a specific workflow ID",
  inputSchema: {
    workflowId: z.optional(z.string()),
    workflowType: z.optional(z.string()),
    limit: z.optional(z.number()),
  },
  async execute(ctx, input) {
    // Create Temporal client
    const connection = await Connection.connect({
      address: ctx.env?.TEMPORAL_GRPC_ENDPOINT,
      tls: true,
      apiKey: ctx.env?.TEMPORAL_API_KEY,
    });
   const client = new Client({
      connection,
      namespace: ctx.env?.TEMPORAL_NAMESPACE,
    });

    // Build query string
    const queryParts = [];
    if (input.workflowId) {
      queryParts.push(`WorkflowId = "${input.workflowId}"`);
    }
    if (input.workflowType) {
      queryParts.push(`WorkflowType = "${input.workflowType}"`);
    }
    const query = queryParts.length > 0 ? queryParts.join(' AND ') : undefined;

    // Limit settings
    const limit = input.limit ?? 25;

    // List workflow executions
    const workflows = [];

    for await (const workflow of client.workflow.list({
      query,
      pageSize: limit,
    })) {
      workflows.push({
        workflowId: workflow.workflowId,
        runId: workflow.runId,
        workflowType: workflow.type,
        status: workflow.status,
        startTime: workflow.startTime,
        closeTime: workflow.closeTime,
        executionTime: workflow.executionTime,
      });

      if (workflows.length >= limit) {
        break;
      }
    }

    return ctx.json({
      query: query || "all workflows",
      workflows,
      count: workflows.length,
    });
  },
})
.tool({
  name: "get_workflow_details",
  description: "Get detailed information about a specific Temporal workflow execution. If runId is not provided, gets the latest run for the workflow.",
  inputSchema: {
    workflowId: z.string(),
  },
  async execute(ctx, input) {
    // Create Temporal client
    const connection = await Connection.connect({
      address: ctx.env?.TEMPORAL_GRPC_ENDPOINT,
      tls: true,
      apiKey: ctx.env?.TEMPORAL_API_KEY,
    });
    const client = new Client({
      connection,
      namespace: ctx.env?.TEMPORAL_NAMESPACE,
    });

    try {
      // Get workflow handle (gets latest run if runId not specified)
      const handle = client.workflow.getHandle(input.workflowId);

      // Fetch workflow details
      const description = await handle.describe();

      return ctx.json(description);
    } catch (error) {
      if (error instanceof Error && error.name === 'WorkflowNotFoundError') {
        ctx.fail(
          { error: `Workflow not found: ${input.workflowId}` },
          { status: 404 }
        );
      }
      throw error;
    }
  },
});

export default gram;
