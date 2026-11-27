import type { Gram as SpeakeasyClient } from "@gram/client";
import { expect, it, vi } from "vitest";
import { provisionKitSlackAccess } from "./kitSlackAccess";

function fixture(canWrite = true) {
  const get = vi.fn().mockResolvedValue({ permissions: { write: canWrite } });
  const createToolset = vi.fn().mockResolvedValue({ id: "toolset" });
  const createServer = vi.fn().mockResolvedValue({
    id: "server",
    projectId: "project",
    slug: "casey-slack",
  });
  const createPolicyGrant = vi.fn().mockResolvedValue({});
  const sdk = {
    agents: { get, createPolicyGrant },
    mcpServers: { create: createServer },
    toolsets: { create: createToolset },
  } as unknown as SpeakeasyClient;
  return { sdk, createToolset, createServer, createPolicyGrant };
}
const inputs = {
  agentId: "agent",
  assistantName: "Casey",
  name: "Casey Slack",
  toolUrns: ["tools:http:slack:reply"],
  environment: { id: "env", slug: "casey-env" },
};

it("provisions only approved capabilities and one project-scoped grant", async () => {
  const { sdk, createToolset, createServer, createPolicyGrant } = fixture();
  await expect(provisionKitSlackAccess(sdk, inputs)).resolves.toBe(
    "casey-slack",
  );
  expect(createToolset).toHaveBeenCalledWith({
    createToolsetRequestBody: {
      name: "Casey Slack",
      description: "Slack capabilities for Casey.",
      toolUrns: inputs.toolUrns,
      defaultEnvironmentSlug: "casey-env",
    },
  });
  expect(createServer).toHaveBeenCalledWith({
    createMcpServerForm: {
      name: "Casey Slack",
      toolsetId: "toolset",
      environmentId: "env",
      visibility: "private",
    },
  });
  expect(createPolicyGrant).toHaveBeenCalledWith({
    createAgentPolicyGrantForm: {
      agentId: "agent",
      effect: "allow",
      scope: "mcp:connect",
      selector: {
        resourceKind: "mcp",
        resourceId: "toolset",
        projectId: "project",
      },
    },
  });
});

it("does not create a server when the setup user cannot edit the backing agent", async () => {
  const { sdk, createToolset, createServer } = fixture(false);
  await expect(provisionKitSlackAccess(sdk, inputs)).rejects.toThrow(
    "permission",
  );
  expect(createToolset).not.toHaveBeenCalled();
  expect(createServer).not.toHaveBeenCalled();
});
