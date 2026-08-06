import { getServerURL } from "@/lib/utils";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { Network } from "lucide-react";

const sizeClasses = {
  // Table/list rows.
  sm: {
    image: "h-6 w-6 object-contain",
    fallback: "text-muted-foreground h-5 w-5",
  },
  // Grid cards.
  lg: {
    image: "h-12 w-12 object-contain",
    fallback: "text-muted-foreground h-8 w-8",
  },
} as const;

/**
 * Renders the logo persisted on an mcp_servers row's MCP metadata (e.g. the
 * catalog icon captured at install time), falling back to the generic network
 * glyph when no logo is set. Metadata is fetched per server and cached by
 * React Query; a missing metadata row is expected and renders the fallback.
 */
export function McpServerLogo({
  mcpServerId,
  name,
  size,
  fallback = "glyph",
}: {
  mcpServerId: string;
  name?: string | null;
  size: keyof typeof sizeClasses;
  /** "none" renders nothing when no logo is set (for surfaces that show no icon today). */
  fallback?: "glyph" | "none";
}): JSX.Element | null {
  const { data } = useGetMcpMetadata({ mcpServerId }, undefined, {
    retry: false,
    throwOnError: false, // Expected 404 when no metadata exists
  });

  const logoAssetId = data?.metadata?.logoAssetId;
  if (!logoAssetId) {
    if (fallback === "none") return null;
    return <Network className={sizeClasses[size].fallback} />;
  }

  return (
    <img
      src={`${getServerURL()}/rpc/assets.serveImage?id=${logoAssetId}`}
      alt={name ?? "MCP server logo"}
      className={sizeClasses[size].image}
    />
  );
}
