import { useMemo } from "react";
import { useListScopes } from "@gram/client/react-query/listScopes.js";
import type { ScopeTask } from "@gram/client/models/components/scopetask.js";
import {
  scopeCatalogue,
  type ScopeCatalogue,
} from "@/pages/access/scopeCatalogue";

const NO_TASKS: ScopeTask[] = [];

/**
 * The scope catalogue and task list from access.listScopes. Callers that
 * only label a scope degrade to the slug while it loads or when the caller
 * may not read it, so nothing here throws. Callers that decide something from
 * the catalogue, such as which roles fit a request, check `isError` so an
 * empty catalogue is not mistaken for an empty answer.
 */
export function useScopeCatalogue(): {
  catalogue: ScopeCatalogue;
  tasks: ScopeTask[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
} {
  const { data, isLoading, isError, refetch } = useListScopes();
  const catalogue = useMemo(() => scopeCatalogue(data?.scopes), [data?.scopes]);
  return {
    catalogue,
    tasks: data?.tasks ?? NO_TASKS,
    isLoading,
    isError,
    refetch: () => void refetch(),
  };
}
