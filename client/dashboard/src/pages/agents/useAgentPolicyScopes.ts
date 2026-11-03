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
  const { data, isLoading, isError } = useListAgentPolicyScopes();
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
