import { mcpServerRouteParam } from "@/lib/sources";
import { Button } from "@/components/ui/Button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { useRoutes } from "@/routes";
import { Info, ExternalLink } from "lucide-react";
import type { JSX } from "react";
import type { InventoryServer } from "./useServerInventory";

export function ServerInfoPopover({
  server,
}: {
  server: InventoryServer;
}): JSX.Element {
  const routes = useRoutes({ projectSlug: server.projectSlug });
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="tertiary"
          size="xs"
          aria-label={`About ${server.name}`}
        >
          <Info className="size-4" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="space-y-3 text-sm">
        <p className="font-medium">{server.name}</p>
        <p className="text-muted-foreground break-words">
          {server.description?.trim() || "No description available."}
        </p>
        <dl className="space-y-2">
          <div>
            <dt className="text-muted-foreground">Slug</dt>
            <dd className="break-all font-mono text-xs">
              {server.slug || "Unavailable"}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">
              Tool calls · last 24 hours
            </dt>
            <dd>{server.recentToolCalls?.toLocaleString() ?? "Unavailable"}</dd>
          </div>
        </dl>
        <a
          href={
            server.kind === "Toolset" && server.toolsetSlug
              ? routes.toolset.href(server.toolsetSlug)
              : routes.mcp.details.overview.href(mcpServerRouteParam(server))
          }
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 underline underline-offset-2"
        >
          Open MCP server <ExternalLink className="size-3" aria-hidden="true" />
        </a>
      </PopoverContent>
    </Popover>
  );
}
