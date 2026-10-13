import type { WorkloadTokenEndpoint } from "@gram/client/models/components/workloadtokenendpoint.js";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { useCreateSharedOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/createSharedOrganizationUserSessionIssuer.js";
import {
  invalidateAllWorkloadTokenEndpoints,
  useWorkloadTokenEndpoints,
} from "@gram/client/react-query/workloadTokenEndpoints.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { ComputedValueKey } from "./definition";

/** The issuers that serve a token endpoint, under one owner. */
export interface TokenEndpointGroup {
  /** "Organization", or the owning project's name. */
  label: string;
  options: { id: string; label: string }[];
}

/**
 * Creating an organization-owned shared authorization server when none
 * serves a token endpoint yet. The server picks every setting; the operator
 * only decides to create one.
 */
export interface IssuerCreation {
  /** Whether there is no token endpoint to select, so creating one is offered. */
  offered: boolean;
  creating: boolean;
  /** Why the last attempt failed, or null. Retrying repeats the same attempt. */
  error: string | null;
  create: () => Promise<void>;
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
  /** The selected issuer's project id, or "" for an organization issuer. */
  selectedEndpointProjectId: string;
  onEndpointChange: (id: string) => void;
  creation: IssuerCreation;
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
export function useSetupValues(): SetupValues {
  const queryClient = useQueryClient();
  const query = useWorkloadTokenEndpoints(undefined, undefined, {
    throwOnError: false,
  });
  const [chosenId, setChosenId] = useState("");

  const endpoints = query.data?.items ?? [];
  const selected =
    endpoints.find((e) => e.userSessionIssuerId === chosenId) ?? endpoints[0];

  // One key per attempt, reused on retry: the server derives the issuer from
  // it, so a retry after a timeout lands on the row the first try created
  // rather than a second one. A fresh key is drawn only once the refetched
  // list holds the created issuer, so a click before then repeats the same
  // attempt instead of starting another.
  const [attemptKey, setAttemptKey] = useState(() => crypto.randomUUID());
  const [creating, setCreating] = useState(false);
  const [creationError, setCreationError] = useState<string | null>(null);
  const createIssuer = useCreateSharedOrganizationUserSessionIssuerMutation({
    // The failure is shown beside the button, so the global toast would only
    // repeat it.
    onError: () => {},
  });

  const create = async () => {
    if (creating) return;
    setCreating(true);
    setCreationError(null);
    try {
      const issuer = await createIssuer.mutateAsync({
        request: {
          createSharedOrganizationUserSessionIssuerForm: {
            idempotencyKey: attemptKey,
          },
        },
      });
      await invalidateAllWorkloadTokenEndpoints(queryClient, {
        refetchType: "all",
      });
      setChosenId(issuer.id);
      setAttemptKey(crypto.randomUUID());
    } catch (error) {
      setCreationError(creationFailureMessage(error));
      // Refetched on failure too: a conflict means the attempt's issuer
      // exists already, and the list is what shows it.
      await invalidateAllWorkloadTokenEndpoints(queryClient, {
        refetchType: "all",
      });
    } finally {
      setCreating(false);
    }
  };

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
    selectedEndpointProjectId: selected?.projectId ?? "",
    onEndpointChange: setChosenId,
    creation: {
      offered: !query.isPending && !query.isError && endpoints.length === 0,
      creating,
      error: creationError,
      create,
    },
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
    return "No authorization server serves a shared token endpoint yet. An organization administrator can create one here.";
  }
  return null;
}

/**
 * What to tell the operator when a request fails: a refusal the server
 * explains is shown as written. Anything else, such as a transport error, is
 * reported only as the fallback, since its message describes the client, not
 * the setup.
 */
export function failureMessage(error: unknown, fallback: string): string {
  if (error instanceof ServiceError) {
    return error.message || fallback;
  }
  return fallback;
}

/**
 * Creation's own words for the refusals it can meet: a 403 names who may
 * create one, and a 409 means this attempt's issuer already exists but is no
 * longer usable, so the attempt cannot be repeated as is.
 */
function creationFailureMessage(error: unknown): string {
  if (error instanceof ServiceError) {
    switch (error.statusCode) {
      case 403:
        return "Only an organization administrator can create an authorization server. Ask one to create it, then come back.";
      case 409:
        return "This attempt was already recorded but its authorization server is no longer usable. Reload the page and try again.";
    }
  }
  return failureMessage(error, "Failed to create the authorization server");
}
