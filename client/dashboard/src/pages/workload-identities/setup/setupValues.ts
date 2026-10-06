import type { WorkloadTokenEndpoint } from "@gram/client/models/components/workloadtokenendpoint.js";
import { useWorkloadTokenEndpoints } from "@gram/client/react-query/workloadTokenEndpoints.js";
import { useState } from "react";
import type { ComputedValueKey } from "./definition";

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
}

const ORGANIZATION_GROUP = "Organization";

/** An issuer by its slug, or by its id when it has none. */
function endpointLabel(endpoint: WorkloadTokenEndpoint): string {
  return endpoint.userSessionIssuerSlug || endpoint.userSessionIssuerId;
}

/**
 * Groups endpoints by owner, keeping the server's order: organization-level
 * issuers first, then each project's.
 */
export function groupTokenEndpoints(
  endpoints: WorkloadTokenEndpoint[],
): TokenEndpointGroup[] {
  const groups: TokenEndpointGroup[] = [];
  for (const endpoint of endpoints) {
    const label =
      endpoint.projectId === ""
        ? ORGANIZATION_GROUP
        : endpoint.projectName || endpoint.projectId;
    let group = groups.find((g) => g.label === label);
    if (group === undefined) {
      group = { label, options: [] };
      groups.push(group);
    }
    group.options.push({
      id: endpoint.userSessionIssuerId,
      label: endpointLabel(endpoint),
    });
  }
  return groups;
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
export function useSetupValues(): SetupValues {
  const query = useWorkloadTokenEndpoints(undefined, undefined, {
    throwOnError: false,
  });
  const [chosenId, setChosenId] = useState("");

  const endpoints = query.data?.items ?? [];
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
    ),
    endpointGroups: groupTokenEndpoints(endpoints),
    selectedEndpointId: selected?.userSessionIssuerId ?? "",
    onEndpointChange: setChosenId,
  };
}

function unavailableReason(
  isPending: boolean,
  isError: boolean,
  count: number,
): string | null {
  if (isPending) return "Loading token endpoints…";
  if (isError) return "Speakeasy couldn't load its token endpoints. Try again.";
  if (count === 0) {
    return "No user session issuer serves a shared token endpoint yet. Ask your Speakeasy contact to enable one for your organization or a project.";
  }
  return null;
}
