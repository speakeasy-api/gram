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
   * The shared authorization server recommended for this agent: the one most
   * of its servers already use, else the organization's own, else the only
   * one there is. "" when none is available.
   */
  recommendedEndpointId: string;
  /**
   * The agent's servers that won't accept tokens from the chosen shared
   * authorization server, or null when they all will. An agent running on a
   * platform gets one token, and only servers on that server accept it.
   */
  mismatch: ProviderMismatch | null;
  /**
   * True while the providers are still loading for servers that need
   * checking, when `mismatch` can't be known yet.
   */
  checking: boolean;
}

/** The agent's servers that the chosen shared authorization server can't sign in to. */
export interface ProviderMismatch {
  /** The chosen shared authorization server, or null when none is available. */
  target: { id: string; slug: string } | null;
  /**
   * Each server not on it, with the shared authorization server it uses now,
   * or null when it has its own per-server sign-in.
   */
  servers: { server: SetupServer; current: string | null }[];
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
 * A token from a shared authorization server is accepted only by the MCP
 * servers that sign in through it, so an agent's servers must all be on the
 * one chosen. Every one available is offered, the recommended one first
 * chosen; while some servers aren't on the chosen one there are no values to
 * show, and `mismatch` names those servers.
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
  // Recommend the one most of the servers already use, so the fewest move.
  const usedBy = (endpoint: WorkloadTokenEndpoint) =>
    signingIn.filter(
      (server) => server.issuerId === endpoint.userSessionIssuerId,
    ).length;
  const most = Math.max(0, ...inScope.map(usedBy));
  const favored =
    most > 0
      ? inScope.filter((endpoint) => usedBy(endpoint) === most)
      : inScope;
  // With servers to move, the organization's own breaks a tie; with none, the
  // list's own order stands.
  const recommended =
    (signingIn.length > 0
      ? favored.find((endpoint) => endpoint.projectId === "")
      : undefined) ?? favored[0];
  const selected =
    inScope.find((endpoint) => endpoint.userSessionIssuerId === chosenId) ??
    recommended;
  const offTarget = signingIn.filter(
    (server) => server.issuerId !== selected?.userSessionIssuerId,
  );
  const mismatch: ProviderMismatch | null =
    offTarget.length > 0
      ? {
          target: selected
            ? {
                id: selected.userSessionIssuerId,
                slug: endpointLabel(selected),
              }
            : null,
          servers: offTarget.map((server) => {
            const current = shared.get(server.issuerId!);
            return {
              server,
              current: current ? endpointLabel(current) : null,
            };
          }),
        }
      : null;
  const endpoints = inScope;

  return {
    recommendedEndpointId: recommended?.userSessionIssuerId ?? "",
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
        ? "Some of this agent's servers aren't on the shared authorization server chosen for it, so its token won't work at them."
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
