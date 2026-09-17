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
