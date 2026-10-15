import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { getServerURL, cn } from "@/lib/utils";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";

/** Branding belongs to the server's project, independent of the selected project. */
export function AssignmentServerIcon({
  server,
  className,
}: {
  server: AccessibleMCPServer;
  className?: string;
}): JSX.Element {
  const { data } = useGetMcpMetadata(
    { mcpServerId: server.id, gramProject: server.projectSlug },
    undefined,
    {
      retry: false,
      retryOnMount: false,
      staleTime: 5 * 60 * 1000,
      throwOnError: false,
    },
  );
  const assetId = data?.metadata?.logoAssetId;
  return (
    <Avatar className={cn("bg-background size-8 shrink-0 border", className)}>
      {assetId && (
        <AvatarImage
          src={`${getServerURL()}/rpc/assets.serveImage?id=${encodeURIComponent(assetId)}`}
          alt=""
          className="object-contain p-1"
        />
      )}
      <AvatarFallback className="text-xs">
        {Array.from(server.name.trim())[0]?.toUpperCase() || "M"}
      </AvatarFallback>
    </Avatar>
  );
}
