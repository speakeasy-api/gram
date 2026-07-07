import { useSdkClient } from "@/contexts/Sdk";
import { randomSlugSuffix } from "@/lib/slug";
import type { McpServer } from "@gram/client/models/components";
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
// the remote-mcp and tunneled-mcp create flows.
export async function createDefaultMcpEndpoint(
  client: SdkClient,
  mcpServer: McpServer,
  orgSlug: string | undefined,
): Promise<void> {
  if (!orgSlug) {
    toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
    return;
  }

  try {
    await client.mcpEndpoints.create({
      createMcpEndpointForm: {
        mcpServerId: mcpServer.id,
        slug: `${orgSlug}-${randomSlugSuffix()}`,
      },
    });
  } catch {
    toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
  }
}
