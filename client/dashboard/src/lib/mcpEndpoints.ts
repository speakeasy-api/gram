import { useSdkClient } from "@/contexts/Sdk";
import { randomSlugSuffix } from "@/lib/slug";
import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { toast } from "sonner";

type SdkClient = ReturnType<typeof useSdkClient>;

const DEFAULT_ENDPOINT_FAILED_MESSAGE =
  "MCP server created, but the default endpoint failed. Add one from the server page.";

// Create the default MCP endpoint for a freshly created server. Slugs are
// prefixed with the org slug; a short random suffix keeps them unique.
//
// Best-effort: a failure here leaves the source intact and only surfaces a
// warning. The endpoint is a convenience and can always be added later from
// the server detail page, so it should never roll back the source. Shared by
// the remote-mcp, tunneled-mcp, and catalog install flows. Returns the created
// endpoint so callers can surface its URL, or undefined on failure.
export async function createDefaultMcpEndpoint(
  client: SdkClient,
  mcpServer: McpServer,
  orgSlug: string | undefined,
  options?: RequestOptions,
): Promise<McpEndpoint | undefined> {
  if (!orgSlug) {
    toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
    return undefined;
  }

  try {
    return await client.mcpEndpoints.create(
      {
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: `${orgSlug}-${randomSlugSuffix()}`,
        },
      },
      undefined,
      options,
    );
  } catch {
    toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
    return undefined;
  }
}
