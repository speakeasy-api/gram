import type { Gram } from "@gram/client";

/** Slack setup creates a dedicated server for the capabilities accepted on the
 * setup card. The private member and its Connect grant are enough for the
 * project gateway; no standalone endpoint or assistant attachment is needed. */
export async function provisionKitSlackAccess(
  sdk: Pick<Gram, "agents" | "mcpServers" | "toolsets">,
  inputs: {
    agentId?: string;
    assistantName: string;
    name: string;
    toolUrns: string[];
    environment: { id: string; slug: string };
  },
): Promise<string> {
  const { agentId, assistantName, name, toolUrns, environment } = inputs;
  if (!agentId)
    throw new Error(
      "Set up the assistant’s agent identity before adding Slack tools.",
    );
  const agent = await sdk.agents.get({ id: agentId });
  if (!agent.permissions.write) {
    throw new Error(
      "You need permission to change this assistant’s agent to set up Slack tools.",
    );
  }
  const toolset = await sdk.toolsets.create({
    createToolsetRequestBody: {
      name,
      description: `Slack capabilities for ${assistantName}.`,
      toolUrns,
      defaultEnvironmentSlug: environment.slug,
    },
  });
  const server = await sdk.mcpServers.create({
    createMcpServerForm: {
      name,
      toolsetId: toolset.id,
      environmentId: environment.id,
      visibility: "private",
    },
  });
  try {
    await sdk.agents.createPolicyGrant({
      createAgentPolicyGrantForm: {
        agentId,
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: toolset.id,
          projectId: server.projectId,
        },
      },
    });
  } catch (cause) {
    throw new Error(
      `The Slack MCP server ${name} was created, but the agent’s Connect grant could not be saved. Check the server in the assistant’s Overview before retrying.`,
      { cause },
    );
  }
  return server.slug ?? name;
}
