import { Icon } from "@/components/ui/Icon";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { RequireScope } from "@/components/require-scope";
import { useOrganization, useProject } from "@/contexts/Auth";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { useRBAC } from "@/hooks/useRBAC";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { useRoutes } from "@/routes";
import { usePlugins } from "@gram/client/react-query/plugins";
import { Link } from "react-router";
import type { HostedServerRef } from "./MCPOverviewTab";
import { serverMatchesRef } from "./serverMatchesRef";

const LINK_CLASS =
  "text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground";

/**
 * One quiet line on the Overview saying which plugins hand this server to the
 * team, with a link to the Plugins section of the server's settings. People
 * who cannot edit plugins see it greyed out, with the way to ask for access.
 */
export function PluginDistributionLine({
  server,
  settingsHref,
}: {
  /** The identity plugin membership is keyed by. */
  server: HostedServerRef;
  /** The Plugins section of this server's settings. */
  settingsHref: string;
}): React.JSX.Element | null {
  const canWritePlugins = usePluginWriteAccess();
  const { hasAnyScope, isLoading } = useRBAC();
  const organization = useOrganization();
  const project = useProject();
  if (canWritePlugins) {
    return <LoadedPluginDistributionLine server={server} href={settingsHref} />;
  }
  // Nothing is known about the viewer yet; don't flash the greyed line.
  if (isLoading) return null;
  // The plugin list is open to organization readers; anyone else would only
  // be refused, so they get the line without a count.
  const canReadPlugins = hasAnyScope(
    ["org:read", "org:admin"],
    organization.id,
  );
  return (
    <RequireScope
      scope="plugin:write"
      resourceId={project.id}
      projectId={project.id}
      level="component"
      reason="You don't have permission to manage this server's plugins."
    >
      {canReadPlugins ? (
        <LoadedPluginDistributionLine server={server} />
      ) : (
        <PluginDistributionRow />
      )}
    </RequireScope>
  );
}

/** Reads the project's plugins; without `href` the line is read-only. */
function LoadedPluginDistributionLine({
  server,
  href,
}: {
  server: HostedServerRef;
  href?: string;
}): React.JSX.Element | null {
  const scope = usePluginQueryScope();
  // A failed read leaves the count out rather than failing the Overview.
  const { data } = usePlugins(scope, undefined, { throwOnError: false });
  if (!data) return href ? null : <PluginDistributionRow />;

  // Alphabetical, as in the Settings picker.
  const memberPlugins = data.plugins
    .filter((plugin) =>
      plugin.servers?.some((s) => serverMatchesRef(s, server)),
    )
    .sort((a, b) =>
      a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
    );
  return <PluginDistributionRow memberPlugins={memberPlugins} href={href} />;
}

function PluginDistributionRow({
  memberPlugins,
  href,
}: {
  /** Left out when the viewer cannot read the project's plugins. */
  memberPlugins?: { id: string; name: string }[];
  /** Left out for people who cannot edit plugins: nothing is clickable. */
  href?: string;
}): React.JSX.Element {
  const routes = useRoutes();
  const count = memberPlugins?.length;
  const countLabel = `${count} ${count === 1 ? "plugin" : "plugins"}`;
  const action = count === 0 ? "Add to a plugin" : "Manage plugins";

  return (
    <div className="text-muted-foreground flex min-w-0 items-center gap-2 text-sm">
      <Icon name="puzzle" className="size-3.5 shrink-0" />
      {memberPlugins === undefined ? null : count === 0 ? (
        <span>Not in a plugin yet</span>
      ) : (
        <span className="flex items-center gap-1.5">
          Distributed in
          {href ? (
            <Popover>
              <PopoverTrigger asChild>
                <button
                  type="button"
                  className="text-foreground decoration-muted-foreground hover:decoration-foreground data-[state=open]:decoration-foreground cursor-pointer underline decoration-dotted decoration-[1.5px] underline-offset-4"
                >
                  {countLabel}
                </button>
              </PopoverTrigger>
              <PopoverContent align="start" className="w-56 p-1">
                <ul aria-label="Plugins this server is in">
                  {memberPlugins.map((plugin) => (
                    <li key={plugin.id}>
                      <Link
                        to={routes.plugins.detail.href(plugin.id)}
                        className="hover:bg-muted flex items-center justify-between gap-2 px-3 py-1.5 text-sm"
                      >
                        <span className="truncate">{plugin.name}</span>
                        <Icon
                          name="chevron-right"
                          className="text-muted-foreground size-3.5 shrink-0"
                        />
                      </Link>
                    </li>
                  ))}
                </ul>
              </PopoverContent>
            </Popover>
          ) : (
            <span>{countLabel}</span>
          )}
        </span>
      )}
      {memberPlugins !== undefined && <span aria-hidden="true">·</span>}
      {href ? (
        <Link to={href} className={LINK_CLASS}>
          {action}
        </Link>
      ) : (
        <span className={LINK_CLASS} aria-disabled="true">
          {action}
        </span>
      )}
    </div>
  );
}
