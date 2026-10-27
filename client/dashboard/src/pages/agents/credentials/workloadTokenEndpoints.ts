import type { WorkloadTokenEndpoint } from "@gram/client/models/components/workloadtokenendpoint.js";
import { useWorkloadTokenEndpoints } from "@gram/client/react-query/workloadTokenEndpoints.js";
import { useState } from "react";
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
 * These come from the server's own derivation, the one its authorization
 * server metadata publishes, and are never assembled here: a second derivation
 * would drift from what a client discovers and show values that fail.
 */
export function useSetupValues(targetProjectId?: string): SetupValues {
  const query = useWorkloadTokenEndpoints(undefined, undefined, {
    throwOnError: false,
  });
  const [chosenId, setChosenId] = useState("");

  const endpoints = (query.data?.items ?? []).filter(
    (endpoint) =>
      targetProjectId === undefined ||
      endpoint.projectId === "" ||
      endpoint.projectId === targetProjectId,
  );
  const selected =
    endpoints.find((e) => e.userSessionIssuerId === chosenId) ?? endpoints[0];

  return {
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
    return "This organization has no shared Speakeasy authorization server yet, so Speakeasy can't fill in these values. Ask your Speakeasy contact to turn one on for your organization or a project.";
  }
  return null;
}
