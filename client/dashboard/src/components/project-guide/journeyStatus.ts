import type { JourneyStatus } from "@/components/project-guide/journeys";
import { normalizeRemoteUrl } from "@/pages/catalog/remotes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerActivity } from "@gram/client/models/components/mcpserveractivity.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";

type CatalogServerIdentity = {
  remotes?: Array<{ transportType: string; url: string }>;
};

export function catalogBackedMcpServers(
  servers: McpServer[] | undefined,
  remoteMcpServers: RemoteMcpServer[] | undefined,
  catalogServers: CatalogServerIdentity[] | undefined,
): McpServer[] {
  if (!servers || !remoteMcpServers || !catalogServers) return [];

  const catalogUrls = new Set(
    catalogServers.flatMap((server) =>
      (server.remotes ?? [])
        .filter((remote) => remote.transportType === "streamable-http")
        .map((remote) => normalizeRemoteUrl(remote.url)),
    ),
  );
  const catalogRemoteIds = new Set(
    remoteMcpServers
      .filter((remote) => catalogUrls.has(normalizeRemoteUrl(remote.url)))
      .map((remote) => remote.id),
  );

  return servers.filter(
    (server) =>
      server.remoteMcpServerId !== undefined &&
      catalogRemoteIds.has(server.remoteMcpServerId),
  );
}

export function hasDefaultPluginServer(
  plugins: Plugin[] | undefined,
  mcpServerId: string | undefined,
): boolean {
  return Boolean(
    mcpServerId &&
    plugins?.some(
      (plugin) =>
        plugin.isDefault === true &&
        plugin.servers?.some((server) => server.mcpServerId === mcpServerId),
    ),
  );
}

export function hasMcpServerActivity(
  activity: McpServerActivity[] | undefined,
  server: McpServer | undefined,
): boolean {
  return Boolean(
    server?.slug &&
    activity?.some(
      (entry) =>
        entry.targetType === "hosted_mcp_server" &&
        entry.targetId === server.slug &&
        entry.totalToolCalls > 0,
    ),
  );
}

export function latestSecretsFinding(
  results: RiskResult[] | undefined,
): RiskResult | undefined {
  if (!results?.length) return undefined;
  return results?.reduce((latest, result) =>
    result.createdAt > latest.createdAt ? result : latest,
  );
}

/**
 * Journey B's own artifact. `gitleaks` is how the `secrets` category is stored
 * on a policy's `sources` (see policyToCategories in pages/security).
 */
export function hasBlockingSecretsPolicy(
  policies: RiskPolicy[] | undefined,
): boolean {
  return (policies ?? []).some(
    (policy) =>
      policy.enabled &&
      policy.action === "block" &&
      policy.sources.includes("gitleaks") &&
      (!policy.messageTypes?.length ||
        (policy.messageTypes.includes("tool_request") &&
          policy.messageTypes.includes("tool_response"))),
  );
}

/**
 * Card status from backend state alone, never from local flags, so a user who
 * did some of this yesterday is credited for it. A recorded win outranks a
 * missing start signal: someone who blocked a prompt and later deleted the
 * policy has still had the win.
 */
export function deriveJourneyStatus(signals: {
  startSignal: boolean;
  winSignal: boolean;
}): JourneyStatus {
  if (signals.winSignal) return "done";
  return signals.startSignal ? "in-progress" : "not-started";
}

/**
 * Which step an expanded card opens on. Resolution is card-level for now: the
 * middle steps (server added to the Default plugin, observability plugin
 * installed) have no cheap signal, and land with the journey bodies.
 */
export function firstIncompleteStepIndex(
  status: JourneyStatus,
  stepCount: number,
): number {
  if (status === "done") return stepCount;
  return status === "in-progress" ? 1 : 0;
}
