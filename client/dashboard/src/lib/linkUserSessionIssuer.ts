import type { Gram } from "@gram/client";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { mcpServerUpdateForm } from "@/lib/mcpServerUpdateForm";

type SdkClient = Pick<Gram, "mcpServers" | "toolsets">;

async function updateServerIssuer(
  client: SdkClient,
  latest: McpServer,
  userSessionIssuerId: string,
  options?: RequestOptions,
): Promise<boolean> {
  const { visibility, ...form } = mcpServerUpdateForm(latest);
  if (!visibility) return false;
  await client.mcpServers.update(
    { updateMcpServerForm: { ...form, visibility, userSessionIssuerId } },
    undefined,
    options,
  );
  return true;
}

export async function linkMcpServerUserSessionIssuer(
  client: SdkClient,
  mcpServerId: string,
  userSessionIssuerId: string,
  options?: RequestOptions,
): Promise<void> {
  const latest = await client.mcpServers.get(
    { id: mcpServerId },
    undefined,
    options,
  );
  if (
    !(await updateServerIssuer(client, latest, userSessionIssuerId, options))
  ) {
    throw new Error(
      "This server's visibility is not supported by this version of Speakeasy. Refresh the page before editing.",
    );
  }
}

// Falls back to the toolset call only while the toolset has no canonical MCP server.
export async function linkToolsetUserSessionIssuer(
  client: SdkClient,
  toolset: { id: string; slug: string },
  userSessionIssuerId: string,
  options?: RequestOptions,
): Promise<void> {
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

  if (
    canonical &&
    (await updateServerIssuer(client, canonical, userSessionIssuerId, options))
  ) {
    return;
  }

  await client.toolsets.setUserSessionIssuer(
    {
      slug: toolset.slug,
      setUserSessionIssuerRequestBody: { userSessionIssuerId },
    },
    undefined,
    options,
  );
}
