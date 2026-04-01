import type { ServerNameOverride } from "@gram/client/models/components";
import {
  invalidateAllHooksServerNamesListServerNameOverrides as invalidateServerNamesList,
  useHooksServerNamesDeleteServerNameOverrideMutation as useDeleteServerNameOverrideMutation,
  useHooksServerNamesListServerNameOverrides as useListServerNameOverrides,
  useHooksServerNamesUpsertServerNameOverrideMutation as useUpsertServerNameOverrides,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { toast } from "sonner";

export type ServerNameMappings = {
  overrides: ServerNameOverride[];
  rawToDisplay: Map<string, string>;
  displayToRaws: Map<string, string[]>;
  displayToOverrides: Map<string, ServerNameOverride[]>;
  isLoading: boolean;
  error: Error | null;
  upsert: ReturnType<typeof useUpsertServerNameOverrides>;
  isUpserting: boolean;
  remove: ReturnType<typeof useDeleteServerNameOverrideMutation>;
  isDeleting: boolean;
};

/**
 * Hook for managing server name display overrides in the hooks UI.
 *
 * Provides:
 * - rawToDisplay: Map from raw server names to display names
 * - displayToRaws: Map from display names to arrays of raw server names (for grouping)
 * - upsert: Function to create or update an override
 * - remove: Function to delete an override
 *
 * Example usage:
 * ```tsx
 * const { rawToDisplay, displayToRaws, upsert, remove } = useServerNameMappings(projectSlug);
 *
 * // Show display name in UI
 * const displayName = rawToDisplay.get(rawServerName) ?? rawServerName;
 *
 * // When filtering, expand display name to all raw names
 * const rawNames = displayToRaws.get(displayName) ?? [displayName];
 * ```
 */
export function useServerNameMappings(): ServerNameMappings {
  const queryClient = useQueryClient();

  // Fetch all overrides for the project
  const { data: overrides, ...queryState } = useListServerNameOverrides();

  // Create mapping from raw server name to display name
  const rawToDisplay = useMemo(() => {
    const map = new Map<string, string>();
    if (!overrides) return map;

    for (const override of overrides) {
      map.set(override.rawServerName, override.displayName);
    }
    return map;
  }, [overrides]);

  // Create reverse mapping: display name -> array of raw server names
  // This is useful for filtering when a display name groups multiple servers
  const displayToRaws = useMemo(() => {
    const map = new Map<string, string[]>();
    if (!overrides) return map;

    for (const override of overrides) {
      const existing = map.get(override.displayName) || [];
      map.set(override.displayName, [...existing, override.rawServerName]);
    }
    return map;
  }, [overrides]);

  // Create a map from display name to all override objects with that display name
  // Useful for editing grouped servers
  const displayToOverrides = useMemo(() => {
    const map = new Map<string, ServerNameOverride[]>();
    if (!overrides) return map;

    for (const override of overrides) {
      const existing = map.get(override.displayName) || [];
      map.set(override.displayName, [...existing, override]);
    }
    return map;
  }, [overrides]);

  // Mutation to upsert an override
  const upsertMutation = useUpsertServerNameOverrides({
    onSuccess: async () => {
      toast.success("Server name override updated");
      await invalidateServerNamesList(queryClient);
    },
  });

  // Mutation to delete an override
  const deleteMutation = useDeleteServerNameOverrideMutation({
    onSuccess: async () => {
      await invalidateServerNamesList(queryClient);
    },
  });

  return {
    // Data
    overrides: overrides || [],
    rawToDisplay,
    displayToRaws,
    displayToOverrides,

    // Query state
    isLoading: queryState.isLoading,
    error: queryState.error,

    // Mutations
    upsert: upsertMutation,
    isUpserting: upsertMutation.isPending,
    remove: deleteMutation,
    isDeleting: deleteMutation.isPending,
  };
}
