import { useOrganization, useUser } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllListEnvironments,
  useCreateEnvironmentMutation,
  useListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useRef } from "react";

export interface UsePlaygroundEnvironmentReturn {
  slug: string;
  exists: boolean;
  storedEntries: { name: string; hasStoredValue: boolean }[];
  save: (
    entriesToUpdate: { name: string; value: string }[],
    entriesToRemove: string[],
  ) => void;
  isSaving: boolean;
}

export function usePlaygroundEnvironment(
  toolset: Toolset,
): UsePlaygroundEnvironmentReturn {
  const user = useUser();
  const organization = useOrganization();
  const queryClient = useQueryClient();

  const slug = `playground-${user.id}-${toolset.slug}`;

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const existingEnvironment = useMemo(
    () => environments.find((env) => env.slug === slug),
    [environments, slug],
  );

  const exists = !!existingEnvironment;

  const storedEntries = useMemo(
    () =>
      (existingEnvironment?.entries ?? []).map((entry) => ({
        name: entry.name,
        hasStoredValue: entry.value.length > 0,
      })),
    [existingEnvironment],
  );

  const createEnvironmentMutation = useCreateEnvironmentMutation({
    onSuccess: () => {
      invalidateAllListEnvironments(queryClient);
    },
  });

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      invalidateAllListEnvironments(queryClient);
    },
  });

  const isSaving =
    createEnvironmentMutation.isPending || updateEnvironmentMutation.isPending;

  // Use a ref to hold the debounce timer
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const save = useCallback(
    (
      entriesToUpdate: { name: string; value: string }[],
      entriesToRemove: string[],
    ) => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }

      debounceTimerRef.current = setTimeout(() => {
        if (!existingEnvironment) {
          // Create new environment with initial entries
          const displayName = user.displayName || user.email;
          createEnvironmentMutation.mutate({
            request: {
              createEnvironmentForm: {
                name: `Playground - ${displayName}`,
                organizationId: organization.id,
                entries: entriesToUpdate,
              },
            },
          });
        } else {
          // Update existing environment
          updateEnvironmentMutation.mutate({
            request: {
              slug,
              updateEnvironmentRequestBody: {
                entriesToUpdate,
                entriesToRemove,
              },
            },
          });
        }
      }, 1000);
    },
    [
      existingEnvironment,
      slug,
      user.displayName,
      user.email,
      organization.id,
      createEnvironmentMutation,
      updateEnvironmentMutation,
    ],
  );

  return {
    slug,
    exists,
    storedEntries,
    save,
    isSaving,
  };
}
