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
