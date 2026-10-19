import { RequirePluginWrite } from "@/components/require-plugin-write";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { PluginSkillDistribution } from "@gram/client/models/components/pluginskilldistribution.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { usePluginServerQueries } from "@/pages/plugins/usePluginServerQueries";
import {
  type HostedServerIndex,
  hostedPluginServers,
  hostedPluginServerToolsetId,
  hostedServerIndex,
  type ServingPlugin,
} from "./mcp-served-skills";
import { SkillDistributionVersionBadge } from "./skill-badges";
import {
  useProjectPluginsWithServers,
  useSkillsOverMcpEnabled,
} from "./use-skills-over-mcp";

/**
 * Lists the plugins an individual skill is distributed to and lets writers
 * revoke plugin distributions. Adding distributions happens through the
 * plugin banner at the top of the skill detail page.
 */
export function SkillDistributionsSection({
  skillId,
}: {
  skillId: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const canWritePlugin = usePluginWriteAccess();
  const routes = useRoutes();
  const distributionsQuery = useSkillDistributionsInfinite(
    { skillId, limit: 50 },
    undefined,
    { throwOnError: false },
  );
  const undistribute = useUndistributeSkillMutation();
  const servedOverMcp = useServedOverMcp();

  const distributions = useMemo(
    () =>
      distributionsQuery.data?.pages.flatMap(
        (page) => page.result.distributions,
      ) ?? [],
    [distributionsQuery.data?.pages],
  );

  const handleUndistribute = async (
    distribution: PluginSkillDistribution,
  ): Promise<void> => {
    if (!canWritePlugin) return;
    try {
      await undistribute.mutateAsync({
        request: {
          undistributeSkillRequestBody: {
            id: skillId,
            pluginId: distribution.pluginId,
          },
        },
      });
      await invalidateAllSkillDistributions(queryClient);
      toast.success(`Removed from ${distribution.pluginName}`);
    } catch (_err) {
      toast.error("Unable to remove distribution");
    }
  };

  if (distributionsQuery.isPending && !distributionsQuery.data) {
    return <Skeleton className="h-24 w-full" />;
  }
  if (distributionsQuery.error && !distributionsQuery.data) {
    return (
      <ErrorAlert
        title="Unable to load distributions"
        error={distributionsQuery.error}
      />
    );
  }

  return (
    <div className="space-y-3">
      {distributions.length === 0 ? (
        <div className="border-border border border-dashed p-6">
          <Text small muted>
            Not distributed to any plugins yet. Use the banner above to
            distribute this skill.
          </Text>
        </div>
      ) : (
        <ul className="border-border bg-card divide-y overflow-hidden border">
          {distributions.map((distribution) => (
            <li
              key={distribution.id}
              className="flex items-center justify-between gap-3 px-4 py-3"
            >
              <div className="min-w-0">
                <Link
                  to={`${routes.plugins.detail.href(distribution.pluginId)}?skillId=${encodeURIComponent(skillId)}`}
                  className="text-sm font-medium hover:underline"
                >
                  {distribution.pluginName}
                </Link>
                <Text
                  small
                  muted
                  className="block text-xs"
                  title={dateTimeFormatters.full.format(distribution.createdAt)}
                >
                  Distributed <HumanizeDateTime date={distribution.createdAt} />
                </Text>
                {servedOverMcp.enabled && (
                  <ServedOverMcpLine
                    pluginId={distribution.pluginId}
                    served={servedOverMcp}
                  />
                )}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <SkillDistributionVersionBadge
                  pinnedVersionId={distribution.pinnedVersionId}
                />
                <RequirePluginWrite>
                  <Button
                    type="button"
                    variant="tertiary"
                    size="sm"
                    tooltip="Remove from plugin"
                    aria-label={`Remove from ${distribution.pluginName}`}
                    className="hover:text-destructive"
                    disabled={undistribute.isPending}
                    onClick={() => void handleUndistribute(distribution)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </RequirePluginWrite>
              </div>
            </li>
          ))}
        </ul>
      )}

      {distributionsQuery.hasNextPage && (
        <Button
          variant="secondary"
          size="sm"
          disabled={distributionsQuery.isFetchingNextPage}
          onClick={() => void distributionsQuery.fetchNextPage()}
        >
          {distributionsQuery.isFetchingNextPage
            ? "Loading..."
            : "Load more distributions"}
        </Button>
      )}
    </div>
  );
}

type ServedOverMcp = {
  enabled: boolean;
  isLoading: boolean;
  /** The plugin list failed; the row says nothing rather than waiting. */
  failed: boolean;
  pluginsById: Map<string, ServingPlugin> | undefined;
  toolsetsById: Map<string, { slug: string; name: string }> | undefined;
  /** Hosted mcp_servers id to the toolset it fronts, for wrapper-keyed servers. */
  hostedIndex: HostedServerIndex | undefined;
};

/**
 * What each plugin row needs to say which hosted MCP servers serve the skill:
 * the project's plugins with their servers, the toolsets those servers
 * resolve to for names and links, and the MCP server list that resolves a
 * wrapper-keyed server to its toolset. All stay unfetched while the flag is off.
 */
function useServedOverMcp(): ServedOverMcp {
  const enabled = useSkillsOverMcpEnabled();
  const { plugins, isLoading, canReadPlugins, error } =
    useProjectPluginsWithServers(enabled);
  const { toolsetsQuery, serversQuery, canReadServers } =
    usePluginServerQueries({ enabled });
  const pluginsById = useMemo(() => {
    if (!plugins) return undefined;
    return new Map(plugins.map((plugin) => [plugin.id, plugin]));
  }, [plugins]);
  const toolsets = toolsetsQuery.data?.toolsets;
  const toolsetsById = useMemo(() => {
    if (!toolsets) return undefined;
    return new Map(
      toolsets.map((toolset) => [
        toolset.id,
        { slug: toolset.slug, name: toolset.name },
      ]),
    );
  }, [toolsets]);
  const hostedIndex = useMemo(() => {
    if (!toolsets) return undefined;
    return hostedServerIndex(serversQuery.data?.mcpServers, toolsets);
  }, [serversQuery.data?.mcpServers, toolsets]);
  return {
    enabled: enabled && canReadPlugins,
    failed: !!error && !plugins,
    isLoading:
      isLoading ||
      (enabled &&
        canReadServers &&
        ((!toolsetsById && toolsetsQuery.isLoading) ||
          (!serversQuery.data && serversQuery.isLoading))),
    pluginsById,
    toolsetsById,
    hostedIndex,
  };
}

/**
 * One line under the plugin name naming the hosted MCP servers that serve the
 * skill to agents, each linked to its server page. A plugin with no hosted
 * server says so, since adding one is what turns serving on. A server the
 * caller cannot resolve (the server list is filtered by their grants) is
 * counted rather than taken as remote, so the line never denies serving it
 * cannot see.
 */
function ServedOverMcpLine({
  pluginId,
  served,
}: {
  pluginId: string;
  served: ServedOverMcp;
}): JSX.Element | null {
  const routes = useRoutes();
  if (served.failed) return null;
  if (served.isLoading || !served.pluginsById) {
    return <Skeleton className="mt-1 h-4 w-48" />;
  }
  const plugin = served.pluginsById.get(pluginId);
  if (!plugin) return null;
  const { servers, unresolved } = hostedPluginServers(
    plugin.servers,
    served.hostedIndex,
  );
  const hiddenCount = unresolved.length;
  const hiddenNoun = hiddenCount === 1 ? "server" : "servers";
  if (servers.length === 0) {
    return (
      <Text small muted className="block text-xs">
        {hiddenCount > 0
          ? `May be served over MCP: this plugin includes ${hiddenCount} MCP ${hiddenNoun} you can't view.`
          : "Not served over MCP: this plugin includes no hosted MCP server."}
      </Text>
    );
  }
  return (
    <Text small muted className="block text-xs">
      Served over MCP by{" "}
      {servers.map((server, index) => {
        const toolsetId = hostedPluginServerToolsetId(
          server,
          served.hostedIndex,
        );
        const toolset = toolsetId
          ? served.toolsetsById?.get(toolsetId)
          : undefined;
        return (
          <span key={server.id}>
            {index > 0 && ", "}
            {toolset ? (
              <Link
                to={routes.mcp.details.href(toolset.slug)}
                className="text-foreground hover:underline"
              >
                {toolset.name}
              </Link>
            ) : (
              <span className="text-foreground">{server.displayName}</span>
            )}
          </span>
        );
      })}
      {hiddenCount > 0 && `, and ${hiddenCount} ${hiddenNoun} you can't view`}
    </Text>
  );
}
