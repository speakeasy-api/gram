import type { ResourceAudienceRolePlugin } from "@gram/client/models/components/resourceaudienceroleplugin.js";
import { useRoutes } from "@/routes";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Puzzle } from "lucide-react";
import { Fragment, type JSX } from "react";
import { Link } from "react-router";

export function RolePluginLinks({
  principalUrn,
  plugins,
}: {
  principalUrn: string;
  plugins: ResourceAudienceRolePlugin[];
}): JSX.Element {
  const routes = useRoutes();
  const matches = plugins
    .filter((plugin) => plugin.principalUrn === principalUrn)
    .sort(
      (a, b) =>
        a.name.localeCompare(b.name) ||
        a.slug.localeCompare(b.slug) ||
        a.pluginId.localeCompare(b.pluginId),
    );
  if (matches.length === 0) {
    return <span className="text-muted-foreground text-sm">—</span>;
  }
  return (
    <div className="text-muted-foreground flex min-w-0 items-start gap-1.5 text-sm">
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={0} aria-label="Plugins" className="mt-0.5 shrink-0">
            <Puzzle className="size-3.5" aria-hidden="true" />
          </span>
        </TooltipTrigger>
        <TooltipContent>Plugins</TooltipContent>
      </Tooltip>
      <span className="min-w-0">
        {matches.map((plugin, index) => (
          <Fragment key={plugin.pluginId}>
            {index > 0 && ", "}
            <Link
              to={routes.plugins.detail.href(plugin.pluginId)}
              className="text-foreground focus-visible:ring-ring rounded-sm text-left underline decoration-dotted underline-offset-4 hover:decoration-solid focus-visible:outline-none focus-visible:ring-2"
            >
              {plugin.name}
              {matches.some(
                (other) =>
                  other.pluginId !== plugin.pluginId &&
                  other.name === plugin.name,
              ) && ` (${plugin.slug})`}
            </Link>
          </Fragment>
        ))}
      </span>
    </div>
  );
}
