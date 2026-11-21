import { Icon } from "@/components/ui/Icon";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
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
 * team, with a link to the Plugins section of the server's settings. Only
 * people who can edit plugins see it, as with that section.
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
  return canWritePlugins ? (
    <WritablePluginDistributionLine
      server={server}
      settingsHref={settingsHref}
    />
  ) : null;
}

function WritablePluginDistributionLine({
  server,
  settingsHref,
}: {
  server: HostedServerRef;
  settingsHref: string;
}): React.JSX.Element | null {
  const routes = useRoutes();
  const scope = usePluginQueryScope();
  // A failed read leaves the line out rather than failing the Overview.
  const { data } = usePlugins(scope, undefined, { throwOnError: false });
  if (!data) return null;

  // Alphabetical, as in the Settings picker.
  const memberPlugins = data.plugins
    .filter((plugin) =>
      plugin.servers?.some((s) => serverMatchesRef(s, server)),
    )
    .sort((a, b) =>
      a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
    );
  const count = memberPlugins.length;

  return (
    <div className="text-muted-foreground flex min-w-0 items-center gap-2 text-sm">
      <Icon name="puzzle" className="size-3.5 shrink-0" />
      {count === 0 ? (
        <span>Not in a plugin yet</span>
      ) : (
        <span className="flex items-center gap-1.5">
          Distributed in
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                className="text-foreground decoration-muted-foreground hover:decoration-foreground data-[state=open]:decoration-foreground cursor-pointer underline decoration-dotted decoration-[1.5px] underline-offset-4"
              >
                {count} {count === 1 ? "plugin" : "plugins"}
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
        </span>
      )}
      <span aria-hidden="true">·</span>
      <Link to={settingsHref} className={LINK_CLASS}>
        {count === 0 ? "Add to a plugin" : "Manage plugins"}
      </Link>
    </div>
  );
}
