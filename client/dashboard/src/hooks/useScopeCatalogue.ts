import { useMemo } from "react";
import { useListScopes } from "@gram/client/react-query/listScopes.js";
import type { ScopeWorkflow } from "@gram/client/models/components/scopeworkflow.js";
import {
  scopeCatalogue,
  type ScopeCatalogue,
} from "@/pages/access/scopeCatalogue";

const NO_WORKFLOWS: ScopeWorkflow[] = [];

/**
 * The scope catalogue and workflow list from access.listScopes. Callers that
 * only label a scope degrade to the slug while it loads or when the caller
 * may not read it, so nothing here throws.
 */
export function useScopeCatalogue(): {
  catalogue: ScopeCatalogue;
  workflows: ScopeWorkflow[];
  isLoading: boolean;
} {
  const { data, isLoading } = useListScopes();
  const catalogue = useMemo(() => scopeCatalogue(data?.scopes), [data?.scopes]);
  return {
    catalogue,
    workflows: data?.workflows ?? NO_WORKFLOWS,
    isLoading,
  };
}
