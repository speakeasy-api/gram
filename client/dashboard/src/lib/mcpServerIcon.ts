import type { RequestOptions } from "@gram/client/lib/sdks.js";
import type { useSdkClient } from "@/contexts/Sdk";

type SdkClient = ReturnType<typeof useSdkClient>;

/**
 * Puts a logo on a freshly created MCP server, from wherever the creating flow
 * knows one: a catalog entry's icon, an identity provider's application logo.
 *
 * Best-effort: a logo failure never fails the create, and the returned promise
 * never rejects. Resolves true only when the logo actually landed on
 * mcp_metadata, so callers know whether a metadata refetch is warranted.
 */
export async function persistServerIconBestEffort(
  client: SdkClient,
  iconUrl: string | undefined,
  mcpServerId: string,
  reqOpts?: RequestOptions,
): Promise<boolean> {
  if (!iconUrl) return false;

  try {
    const uploaded = await client.assets.fetchImageFromURL(
      { fetchImageFromURLForm2: { url: iconUrl } },
      undefined,
      reqOpts,
    );
    await client.mcpMetadata.set(
      {
        setMcpMetadataRequestBody: {
          mcpServerId,
          logoAssetId: uploaded.asset.id,
        },
      },
      undefined,
      reqOpts,
    );
    return true;
  } catch (iconError) {
    console.warn("Failed to persist server icon during install.", {
      mcpServerId,
      iconUrl,
      iconError,
    });
    return false;
  }
}
