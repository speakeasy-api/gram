import { useOrganization, useUser } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllListEnvironments,
  useCreateEnvironmentMutation,
  useListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { toast } from "sonner";

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

  // The slug doubles as the environment name so that the server's
  // conv.ToSlug(name) produces exactly this slug on creation.
  const slug = `playground-${user.id}-${toolset.slug}`;

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const existingEnvironment = useMemo(
    () => environments.find((env) => env.slug === slug),
    [environments, slug],
  );

  const storedEntries = useMemo(
    () =>
      (existingEnvironment?.entries ?? []).map((entry) => ({
        name: entry.name,
        hasStoredValue: entry.value.length > 0,
      })),
    [existingEnvironment],
  );

  const createMutation = useCreateEnvironmentMutation();
  const updateMutation = useUpdateEnvironmentMutation();

  const save = useCallback(
    (
      entriesToUpdate: { name: string; value: string }[],
      entriesToRemove: string[],
    ) => {
      const onSuccess = () => {
        invalidateAllListEnvironments(queryClient);
      };
      const onError = () => {
        toast.error("Failed to save credentials");
      };

      if (!existingEnvironment) {
        createMutation.mutate(
          {
            request: {
              createEnvironmentForm: {
                name: slug,
                organizationId: organization.id,
                entries: entriesToUpdate,
              },
            },
          },
          { onSuccess, onError },
        );
      } else {
        updateMutation.mutate(
          {
            request: {
              slug,
              updateEnvironmentRequestBody: {
                entriesToUpdate,
                entriesToRemove,
              },
            },
          },
          { onSuccess, onError },
        );
      }
    },
    [
      existingEnvironment,
      slug,
      organization.id,
      createMutation,
      updateMutation,
      queryClient,
    ],
  );

  return {
    slug,
    exists: !!existingEnvironment,
    storedEntries,
    save,
    isSaving: createMutation.isPending || updateMutation.isPending,
  };
}
