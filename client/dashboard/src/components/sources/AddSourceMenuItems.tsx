import { DropdownMenuItem } from "@/components/ui/moonshine";
import { useRoutes } from "@/routes";
import { Code, FileCode, Network, Server } from "lucide-react";
import type { ReactNode } from "react";

/**
 * Shared "Add Source" menu body — used by both the populated Sources index
 * (its trigger lives in the section toolbar) and SourcesEmptyState (its
 * trigger is the empty-state CTA). Kept in one place so the source-type
 * catalog can't drift between the two entry points.
 */
export function AddSourceMenuItems({
  isFunctionsEnabled,
  isTunneledMcpEnabled,
}: {
  isFunctionsEnabled: boolean;
  isTunneledMcpEnabled: boolean;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <>
      <AddSourceMenuItem
        onSelect={() => routes.sources.addOpenAPI.goTo()}
        icon={<FileCode className="text-muted-foreground h-5 w-5" />}
        title="From your API"
        description="Upload an OpenAPI spec to generate tools"
      />
      {isFunctionsEnabled && (
        <AddSourceMenuItem
          onSelect={() => routes.sources.addFunction.goTo()}
          icon={<Code className="text-muted-foreground h-5 w-5" />}
          title="Write custom code"
          description="Create tools with TypeScript functions"
        />
      )}
      <AddSourceMenuItem
        onSelect={() => routes.sources.addFromCatalog.goTo()}
        icon={<Server className="text-muted-foreground h-5 w-5" />}
        title="3rd-party server"
        description="Add pre-built servers from the catalog"
      />
      <AddSourceMenuItem
        onSelect={() => routes.sources.addRemoteMcp.goTo()}
        icon={<Network className="text-muted-foreground h-5 w-5" />}
        title="Custom remote server"
        description="Add existing remote servers by URL"
      />
      {isTunneledMcpEnabled && (
        <AddSourceMenuItem
          onSelect={() => routes.sources.addTunneledMcp.goTo()}
          icon={<Network className="text-muted-foreground h-5 w-5" />}
          title="Tunneled MCP Server"
          description="Connect private MCP servers through a tunnel"
        />
      )}
    </>
  );
}

function AddSourceMenuItem({
  onSelect,
  icon,
  title,
  description,
}: {
  onSelect: () => void;
  icon: ReactNode;
  title: string;
  description: string;
}) {
  return (
    <DropdownMenuItem
      onSelect={onSelect}
      className="flex cursor-pointer items-start gap-3 p-2"
    >
      <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center">
        {icon}
      </div>
      <div className="flex flex-col gap-0.5">
        <span className="font-medium">{title}</span>
        <span className="text-muted-foreground text-xs">{description}</span>
      </div>
    </DropdownMenuItem>
  );
}
