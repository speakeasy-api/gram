import { useMemo } from "react";
import { useListAgentPolicyScopes } from "@gram/client/react-query/listAgentPolicyScopes.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";

import {
  scopeCatalogue,
  type ScopeCatalogue,
} from "@/pages/access/scopeCatalogue";
import { isKnownResourceType, type ResourceType } from "@/pages/access/types";

export type AgentPolicyScope = ScopeDefinition & { resourceType: ResourceType };

/**
 * The scopes an agent policy may hold, as the server describes them. The read
 * needs a session and no organization grant, so an owner configuring their
 * own agent gets the same labels and descriptions as the role editor; the
 * dashboard keeps no copy of the runtime-safe set.
 */
export function useAgentPolicyScopes(): {
  scopes: AgentPolicyScope[];
  catalogue: ScopeCatalogue;
  isLoading: boolean;
  isError: boolean;
} {
  // The app-wide query default throws every error but 401/403 to the route
  // error boundary, and this read never returns 403; opting out lets a 5xx
  // (or a 404 while the server is older than the dashboard) reach the
  // editor's own inline error state instead of replacing the whole tab.
  const { data, isLoading, isError } = useListAgentPolicyScopes(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const scopes = useMemo(
    () =>
      (data?.scopes ?? []).filter((scope): scope is AgentPolicyScope =>
        isKnownResourceType(scope.resourceType),
      ),
    [data?.scopes],
  );
  const catalogue = useMemo(() => scopeCatalogue(data?.scopes), [data?.scopes]);
  return { scopes, catalogue, isLoading, isError };
}
