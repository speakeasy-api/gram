import { useInstalledCatalogServer } from "@/pages/catalog/useInstalledCatalogServer";
import { resolveCatalogLogo } from "@/pages/catalog/logo";
import { useIsDarkTheme } from "@/lib/theme";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { getServerURL, cn } from "@/lib/utils";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { ProjectIcon } from "@/components/project-icon";
import { useOrganization } from "@/contexts/Auth";

/** Branding belongs to the server's project, independent of the selected project. */
export function AssignmentServerIcon({
  server,
  className,
}: {
  server: AccessibleMCPServer;
  className?: string;
}): JSX.Element {
  const { data, isFetched } = useGetMcpMetadata(
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
  const catalog = useInstalledCatalogServer(
    server.id,
    isFetched && !assetId,
    server.projectSlug,
  );
  const theme = useIsDarkTheme() ? "dark" : "light";
  const logo =
    isFetched && catalog ? resolveCatalogLogo(catalog, theme) : undefined;
  const src = assetId
    ? `${getServerURL()}/rpc/assets.serveImage?id=${encodeURIComponent(assetId)}`
    : logo?.src;
  return (
    <Avatar className={cn("bg-background size-8 shrink-0 border", className)}>
      {src && (
        <AvatarImage
          src={src}
          alt=""
          className={cn("object-contain p-1", !assetId && logo?.className)}
        />
      )}
      <AvatarFallback className="text-xs">
        {Array.from(server.name.trim())[0]?.toUpperCase() || "M"}
      </AvatarFallback>
    </Avatar>
  );
}

/**
 * The server's project, marked with the same colored initial as the project
 * switcher so it reads as a project rather than as more of the server's name.
 */
export function AssignmentProject({
  server,
  className,
}: {
  server: AccessibleMCPServer;
  className?: string;
}): JSX.Element {
  const organization = useOrganization();
  const project = organization.projects?.find(
    (project) => project.id === server.projectId,
  );
  const name = project?.name || server.projectSlug;
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
      <ProjectIcon
        decorative
        project={{ id: server.projectId, name }}
        className="size-4 text-[9px]"
      />
      <span className="truncate">{name}</span>
    </span>
  );
}
