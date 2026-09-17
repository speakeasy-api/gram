import type { useSdkClient } from "@/contexts/Sdk";

/** Reconcile uncertain writes against the exact retained toolset before retrying. */
export async function ensureToolsetWrapper(
  client: ReturnType<typeof useSdkClient>,
  toolset: { id: string; name: string },
  reconcile: boolean,
): Promise<string> {
  if (reconcile) {
    const { mcpServers } = await client.mcpServers.list({
      toolsetId: toolset.id,
    });
    if (mcpServers.length > 1) {
      throw new Error(
        "Multiple servers use this source. Select the intended existing server.",
      );
    }
    if (mcpServers[0]) return mcpServers[0].id;
    // Read authorization can hide a committed wrapper. An empty result does
    // not prove creation failed, and toolset wrappers are not globally unique.
    throw new Error(
      "The previous server creation could not be confirmed. Ask a project administrator to manually locate and attach the existing server, or confirm that no server was created before starting again.",
    );
  }
  const server = await client.mcpServers.create({
    createMcpServerForm: {
      name: toolset.name,
      toolsetId: toolset.id,
      visibility: "private",
    },
  });
  return server.id;
}
