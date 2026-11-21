import { countMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import type { MemberRow } from "@/pages/mcp/gateway/memberRows";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGetMcpMetadataQuery } from "@gram/client/react-query/getMcpMetadata.core.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { buildToolsetQuery } from "@gram/client/react-query/toolset.core.js";
import { useQueries } from "@tanstack/react-query";

export type GatewayMemberSetupGap = {
  /** The member's server, for linking to its settings. */
  serverId: string;
  serverSlug: string | undefined;
  name: string;
  missingEnvVars: number;
};

/**
 * Hosted members whose required environment variables have no value, so
 * their tools fail when the gateway calls them. One environments list for the
 * project, plus each hosted member's toolset and metadata — the same reads
 * its own detail page makes, so they come from the cache when warm. Gateways
 * have few members.
 */
export function useGatewayMemberSetup(rows: MemberRow[]): {
  gaps: GatewayMemberSetupGap[];
  /** True until every read that could add a gap has settled. */
  loading: boolean;
  /** A read failed, so a gap could be hidden. */
  failed: boolean;
} {
  const client = useGramContext();
  const hosted = rows.flatMap((row) =>
    row.classification === "hosted" && row.server?.toolset?.slug
      ? [{ server: row.server, toolsetSlug: row.server.toolset.slug }]
      : [],
  );
  const environments = useListEnvironments(undefined, undefined, {
    enabled: hosted.length > 0,
    throwOnError: false,
  });
  const toolsets = useQueries({
    queries: hosted.map(({ toolsetSlug }) => ({
      ...buildToolsetQuery(client, { slug: toolsetSlug }),
      throwOnError: false,
    })),
  });
  const metadata = useQueries({
    queries: hosted.map(({ toolsetSlug }) => ({
      ...buildGetMcpMetadataQuery(client, { toolsetSlug }),
      throwOnError: false,
    })),
  });

  const loading =
    hosted.length > 0 &&
    (environments.isPending ||
      toolsets.some((query) => query.isPending) ||
      metadata.some((query) => query.isPending));
  const failed =
    environments.isError ||
    toolsets.some((query) => query.isError) ||
    metadata.some((query) => query.isError);

  const gaps = hosted.flatMap(({ server }, index) => {
    const toolset = toolsets[index]?.data;
    if (!toolset) return [];
    const missingEnvVars = countMissingRequiredEnvVars(
      toolset,
      environments.data?.environments ?? [],
      toolset.defaultEnvironmentSlug || "default",
      metadata[index]?.data?.metadata,
    );
    return missingEnvVars > 0
      ? [
          {
            serverId: server.id,
            serverSlug: server.slug,
            name: server.name || toolset.name,
            missingEnvVars,
          },
        ]
      : [];
  });

  return { gaps, loading, failed };
}
