import { useInstalledCatalogServer } from "@/pages/catalog/useInstalledCatalogServer";
import { CatalogLogo } from "@/pages/catalog/CatalogLogo";
import { AssetImage } from "@/components/asset-image";
import { cn } from "@/lib/utils";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { Network } from "lucide-react";

export function SourceMcpIcon({
  mcpServerId,
  className,
}: {
  mcpServerId: string | undefined;
  className: string;
}): JSX.Element {
  const { data } = useGetMcpMetadata({ mcpServerId }, undefined, {
    enabled: !!mcpServerId,
    // 404 is the normal "no metadata set" answer; don't re-request it on every
    // remount or each navigation replays a console error per server.
    retry: false,
    retryOnMount: false,
    staleTime: 5 * 60 * 1000,
    throwOnError: false,
  });
  const logoAssetId = data?.metadata?.logoAssetId;

  const catalogServer = useInstalledCatalogServer(mcpServerId, !logoAssetId);

  if (logoAssetId) {
    return <AssetImage assetId={logoAssetId} className={className} />;
  }
  if (catalogServer)
    return (
      <CatalogLogo
        server={catalogServer}
        className={className}
        fallback={
          <Network className={cn("text-muted-foreground", className)} />
        }
      />
    );
  return <Network className={cn("text-muted-foreground", className)} />;
}
