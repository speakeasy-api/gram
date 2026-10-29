import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { mcpServerUpdateForm } from "@/lib/mcpServerUpdateForm";

type SdkClient = Pick<Gram, "mcpServers">;

const UNSUPPORTED_VISIBILITY =
  "This server's visibility is not supported by this version of Speakeasy. Refresh the page before editing.";

async function updateServerIssuer(
  client: SdkClient,
  latest: McpServer,
  userSessionIssuerId: string,
  options?: RequestOptions,
): Promise<McpServer> {
  const { visibility, ...form } = mcpServerUpdateForm(latest);
  if (!visibility) throw new Error(UNSUPPORTED_VISIBILITY);
  return await client.mcpServers.update(
    { updateMcpServerForm: { ...form, visibility, userSessionIssuerId } },
    undefined,
    options,
  );
}

export async function linkMcpServerUserSessionIssuer(
  client: SdkClient,
  mcpServerId: string,
  userSessionIssuerId: string,
  options?: RequestOptions,
): Promise<McpServer> {
  const latest = await client.mcpServers.get(
    { id: mcpServerId },
    undefined,
    options,
  );
  return await updateServerIssuer(client, latest, userSessionIssuerId, options);
}

// A toolset's session issuer lives on its canonical MCP server, which shares the toolset's id.
export async function linkToolsetUserSessionIssuer(
  client: SdkClient,
  toolset: { id: string },
  userSessionIssuerId: string,
  options?: RequestOptions,
): Promise<McpServer> {
  let canonical: McpServer | undefined;
  try {
    const server = await client.mcpServers.get(
      { id: toolset.id },
      undefined,
      options,
    );
    if (server.id === toolset.id && server.toolsetId === toolset.id) {
      canonical = server;
    }
  } catch (error) {
    if (!(error instanceof GramError && error.statusCode === 404)) {
      throw error;
    }
  }
  if (!canonical) {
    throw new Error(
      "This toolset has no hosted MCP server, so a sign-in provider cannot be linked to it.",
    );
  }
  return await updateServerIssuer(
    client,
    canonical,
    userSessionIssuerId,
    options,
  );
}
