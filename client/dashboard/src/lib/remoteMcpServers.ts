import { useSdkClient } from "@/contexts/Sdk";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";

type SdkClient = ReturnType<typeof useSdkClient>;

/** The visibilities an MCP server can be created with. */
type Visibility = "disabled" | "private" | "public";

/**
 * Stands a Gram-hosted MCP server in front of a remote one: create the remote
 * MCP server at the given URL, then link an MCP server to it. When the link
 * fails the remote server is deleted again, so a half-made pair never outlives
 * the attempt; when that delete also fails the error says so by name, because
 * the leftover then needs a hand.
 *
 * Only the pair. Everything a particular flow adds on top of it — upstream
 * headers, a logo, OAuth auto-configuration, a pre-staged endpoint — stays with
 * that flow.
 */
export async function createRemoteMcpServerPair(
  client: SdkClient,
  {
    name,
    url,
    visibility,
  }: { name: string; url: string; visibility: Visibility },
  options?: RequestOptions,
): Promise<{ remoteMcpServer: RemoteMcpServer; mcpServer: McpServer }> {
  const remoteMcpServer = await client.remoteMcp.createServer(
    {
      createServerForm: {
        name,
        url,
        transportType: "streamable-http",
      },
    },
    undefined,
    options,
  );

  let mcpServer: McpServer;
  try {
    mcpServer = await client.mcpServers.create(
      {
        createMcpServerForm: {
          name,
          remoteMcpServerId: remoteMcpServer.id,
          visibility,
        },
      },
      undefined,
      options,
    );
  } catch (linkError) {
    try {
      await client.remoteMcp.deleteServer(
        { id: remoteMcpServer.id },
        undefined,
        options,
      );
    } catch (rollbackError) {
      const linkMsg =
        linkError instanceof Error ? linkError.message : String(linkError);
      const rollbackMsg =
        rollbackError instanceof Error
          ? rollbackError.message
          : String(rollbackError);
      throw new Error(
        `Created remote MCP server ${remoteMcpServer.id} but failed to link an MCP server, and the rollback also failed. Delete it manually before retrying. Cause: ${linkMsg}. Rollback: ${rollbackMsg}.`,
      );
    }
    throw linkError instanceof Error ? linkError : new Error(String(linkError));
  }

  return { remoteMcpServer, mcpServer };
}
