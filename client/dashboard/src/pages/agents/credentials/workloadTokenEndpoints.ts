import type { WorkloadTokenEndpoint } from "@gram/client/models/components/workloadtokenendpoint.js";
import { useWorkloadTokenEndpoints } from "@gram/client/react-query/workloadTokenEndpoints.js";
import { useState } from "react";
import type { SetupServer } from "./setupInstructions";
import type { ComputedValueKey } from "./workloadCatalog";

/** The issuers that serve a token endpoint, under one owner. */
export interface TokenEndpointGroup {
  /** "Organization", or the owning project's name. */
  label: string;
  options: { id: string; label: string }[];
}

/**
 * Why a token from the selected issuer is refused by a server: it signs in
 * through another shared issuer, which the reader can select instead, or
 * through one that serves no shared authorization server at all.
 */
type UncoveredReason = "another_issuer" | "not_shared";

export interface UncoveredServer {
  server: SetupServer;
  reason: UncoveredReason;
  /**
   * For a server whose issuer is not shared, the shared issuer it could be
   * moved to: the selected one when the server may use it, otherwise the first
   * at the organization level or in the server's project. Absent when there is
   * none.
   */
  switchTo?: { id: string; label: string };
}

export interface SetupValues {
  /** Absent while there is nothing to show. */
  values?: Partial<Record<ComputedValueKey, string>>;
  /** Why the values cannot be shown, or null when they are. */
  unavailableReason: string | null;
  /** The issuers the operator can point the platform at. */
  endpointGroups: TokenEndpointGroup[];
  /** The selected issuer's id, or "" when there is none to select. */
  selectedEndpointId: string;
  onEndpointChange: (id: string) => void;
  refreshAndSelect: (id: string) => Promise<void>;
  /**
   * The agent's servers a token from the selected issuer works at. Servers
   * that sign no one in are kept, since no issuer decides for them.
   */
  coveredServers: SetupServer[];
  /** The agent's servers that refuse a token from the selected issuer. */
  uncoveredServers: UncoveredServer[];
}

const ORGANIZATION_GROUP = "Organization";

/** An issuer by its slug, or by its id when it has none. */
function endpointLabel(endpoint: WorkloadTokenEndpoint): string {
  return endpoint.userSessionIssuerSlug || endpoint.userSessionIssuerId;
}

/**
 * Groups endpoints by owner, keeping the server's order: organization-level
 * issuers first, then each project's. Groups are keyed by owner id, so two
 * projects sharing a name stay apart; their labels then carry the project id.
 */
export function groupTokenEndpoints(
  endpoints: WorkloadTokenEndpoint[],
): TokenEndpointGroup[] {
  const groups = new Map<string, TokenEndpointGroup & { name: string }>();
  for (const endpoint of endpoints) {
    let group = groups.get(endpoint.projectId);
    if (group === undefined) {
      const name =
        endpoint.projectId === ""
          ? ORGANIZATION_GROUP
          : `Project: ${endpoint.projectName || endpoint.projectId}`;
      group = { name, label: name, options: [] };
      groups.set(endpoint.projectId, group);
    }
    group.options.push({
      id: endpoint.userSessionIssuerId,
      label: endpointLabel(endpoint),
    });
  }
  const all = [...groups.entries()];
  return all.map(([projectId, { name, options }]) => {
    const shared = all.filter(([, other]) => other.name === name).length > 1;
    return { label: shared ? `${name} (${projectId})` : name, options };
  });
}

/**
 * The values a platform is pointed at: the token endpoint and issuer of the
 * user session issuer the operator selects, at the organization level or in a
 * project. Only issuers in shared mode serve one, at /oauth/usi/{id}.
 *
 * A token from a shared issuer is accepted only by the MCP servers that sign
 * in through that issuer. So when the agent's servers are known, only their
 * issuers are offered, the one serving most of them is preselected, and the
 * servers the selection leaves out are reported with why.
 *
 * These come from the server's own derivation, the one its authorization
 * server metadata publishes, and are never assembled here: a second derivation
 * would drift from what a client discovers and show values that fail.
 */
export function useSetupValues(
  targetProjectId?: string,
  servers: SetupServer[] = [],
): SetupValues {
  const query = useWorkloadTokenEndpoints(undefined, undefined, {
    throwOnError: false,
  });
  const [chosenId, setChosenId] = useState("");

  const inScope = (query.data?.items ?? []).filter(
    (endpoint) =>
      targetProjectId === undefined ||
      endpoint.projectId === "" ||
      endpoint.projectId === targetProjectId,
  );
  // Until the issuers load, no server can be said to be left out. Once they
  // have, a failed refetch keeps filtering by the issuers last loaded, so an
  // unrelated issuer is never offered.
  const signingIn =
    query.data === undefined ? [] : servers.filter((server) => server.issuerId);
  const servedBy = (endpoint: WorkloadTokenEndpoint): number =>
    signingIn.filter(
      (server) => server.issuerId === endpoint.userSessionIssuerId,
    ).length;
  const endpoints =
    signingIn.length === 0
      ? inScope
      : inScope.filter((endpoint) => servedBy(endpoint) > 0);
  const selected =
    endpoints.find((e) => e.userSessionIssuerId === chosenId) ??
    endpoints.reduce<WorkloadTokenEndpoint | undefined>(
      (best, endpoint) =>
        best === undefined || servedBy(endpoint) > servedBy(best)
          ? endpoint
          : best,
      undefined,
    );

  const sharedIssuers = new Set(
    endpoints.map((endpoint) => endpoint.userSessionIssuerId),
  );
  const coveredServers = servers.filter(
    (server) =>
      !signingIn.includes(server) ||
      server.issuerId === selected?.userSessionIssuerId,
  );
  // A server may use an issuer at the organization level or in its own project.
  const switchTarget = (server: SetupServer): UncoveredServer["switchTo"] => {
    const usable = inScope.filter(
      (endpoint) =>
        endpoint.projectId === "" || endpoint.projectId === server.projectId,
    );
    const target =
      usable.find(
        (endpoint) =>
          endpoint.userSessionIssuerId === selected?.userSessionIssuerId,
      ) ?? usable[0];
    return target === undefined
      ? undefined
      : { id: target.userSessionIssuerId, label: endpointLabel(target) };
  };
  const uncoveredServers = signingIn
    .filter((server) => server.issuerId !== selected?.userSessionIssuerId)
    .map((server): UncoveredServer =>
      sharedIssuers.has(server.issuerId!)
        ? { server, reason: "another_issuer" }
        : { server, reason: "not_shared", switchTo: switchTarget(server) },
    );

  return {
    coveredServers,
    uncoveredServers,
    values:
      selected === undefined
        ? undefined
        : {
            token_endpoint: selected.tokenEndpoint,
            issuer_url: selected.issuer,
            mcp_host: selected.mcpHost,
          },
    unavailableReason: unavailableReason(
      query.isPending,
      query.isError,
      endpoints.length,
      targetProjectId,
      signingIn.length > 0,
    ),
    endpointGroups: groupTokenEndpoints(endpoints),
    selectedEndpointId: selected?.userSessionIssuerId ?? "",
    onEndpointChange: setChosenId,
    refreshAndSelect: async (id) => {
      const refreshed = await query.refetch({ throwOnError: true });
      const match = refreshed.data?.items.find(
        (endpoint) =>
          endpoint.userSessionIssuerId === id &&
          (targetProjectId === undefined ||
            endpoint.projectId === "" ||
            endpoint.projectId === targetProjectId),
      );
      if (!match)
        throw new Error(
          "The shared authorization server was created, but its exchange endpoint is not available yet. Retry loading the exchange server.",
        );
      setChosenId(id);
    },
  };
}

function unavailableReason(
  isPending: boolean,
  isError: boolean,
  count: number,
  targetProjectId?: string,
  serversSignIn = false,
): string | null {
  if (isPending) return "Loading token endpoints…";
  if (isError) return "Speakeasy couldn't load its token endpoints. Try again.";
  if (count === 0 && serversSignIn) {
    return "None of the servers below signs in through a user session issuer with a shared authorization server, so there is no token endpoint a workload can use for them.";
  }
  if (count === 0 && targetProjectId !== undefined) {
    return "No shared Speakeasy authorization server is available for this project or organization.";
  }
  if (count === 0) {
    return "No usable shared Speakeasy authorization server is available for this organization yet, so Speakeasy can't fill in these values. Ask your Speakeasy contact to set one up for your organization or a project.";
  }
  return null;
}
