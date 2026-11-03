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
   * Why the agent's servers can't all take one token, or null when they can.
   * An agent running on a platform gets a single token, from one MCP
   * authentication provider, and only that provider's servers accept it.
   */
  mismatch: ProviderMismatch | null;
  /**
   * True while the providers are still loading for servers that need
   * checking, when `mismatch` can't be known yet.
   */
  checking: boolean;
}

/** How an agent's servers fall short of sharing one provider. */
export interface ProviderMismatch {
  /** Servers on their own per-server provider, which can't give tokens. */
  ownProvider: SetupServer[];
  /** When the servers use several shared providers, each with its servers. */
  split: { label: string; servers: SetupServer[] }[];
  /**
   * The shared provider every server could move to: the one the others
   * already use, or the only one available. Null when there's a choice to
   * make, such as servers split across shared providers.
   */
  target: { id: string; slug: string } | null;
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
 * in through that issuer, so an agent's servers must all use one. When they
 * do, only that issuer is offered; when they don't, there are no values to
 * show, and `mismatch` says which servers are the problem.
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
  // Only servers that sign people in have a provider. Until the issuers load,
  // none can be said to be on the wrong one.
  const signingIn =
    query.data === undefined ? [] : servers.filter((server) => server.issuerId);
  const shared = new Map(
    inScope.map((endpoint) => [endpoint.userSessionIssuerId, endpoint]),
  );
  const used = inScope.filter((endpoint) =>
    signingIn.some(
      (server) => server.issuerId === endpoint.userSessionIssuerId,
    ),
  );
  const ownProvider = signingIn.filter(
    (server) => !shared.has(server.issuerId!),
  );
  const split =
    used.length > 1
      ? used.map((endpoint) => ({
          label: endpointLabel(endpoint),
          servers: signingIn.filter(
            (server) => server.issuerId === endpoint.userSessionIssuerId,
          ),
        }))
      : [];
  const candidates = used.length > 0 ? used : inScope;
  const target =
    candidates.length === 1
      ? {
          id: candidates[0]!.userSessionIssuerId,
          slug: endpointLabel(candidates[0]!),
        }
      : null;
  const mismatch =
    ownProvider.length > 0 || split.length > 0
      ? { ownProvider, split, target }
      : null;
  const endpoints = used.length > 0 ? used : inScope;
  const selected =
    endpoints.find((e) => e.userSessionIssuerId === chosenId) ?? endpoints[0];

  return {
    mismatch,
    checking: servers.length > 0 && query.isPending,
    values:
      selected === undefined || mismatch !== null
        ? undefined
        : {
            token_endpoint: selected.tokenEndpoint,
            issuer_url: selected.issuer,
            mcp_host: selected.mcpHost,
          },
    unavailableReason:
      mismatch !== null
        ? "This agent's servers don't all use one MCP authentication provider that can give tokens to agents, so there is no token endpoint that works for all of them."
        : unavailableReason(
            query.isPending,
            query.isError,
            endpoints.length,
            targetProjectId,
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
): string | null {
  if (isPending) return "Loading token endpoints…";
  if (isError) return "Speakeasy couldn't load its token endpoints. Try again.";
  if (count === 0 && targetProjectId !== undefined) {
    return "No shared Speakeasy authorization server is available for this project or organization.";
  }
  if (count === 0) {
    return "No usable shared Speakeasy authorization server is available for this organization yet, so Speakeasy can't fill in these values. Ask your Speakeasy contact to set one up for your organization or a project.";
  }
  return null;
}
