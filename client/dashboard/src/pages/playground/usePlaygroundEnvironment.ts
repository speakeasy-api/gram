import { useOrganization, useUser } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllListEnvironments,
  useCreateEnvironmentMutation,
  useListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef } from "react";
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

  // Use refs to avoid stale closures in the debounced callback
  const existingEnvironmentRef = useRef(existingEnvironment);
  existingEnvironmentRef.current = existingEnvironment;

  const slugRef = useRef(slug);
  slugRef.current = slug;

  const orgIdRef = useRef(organization.id);
  orgIdRef.current = organization.id;

  const createMutationRef = useRef(createMutation);
  createMutationRef.current = createMutation;

  const updateMutationRef = useRef(updateMutation);
  updateMutationRef.current = updateMutation;

  const queryClientRef = useRef(queryClient);
  queryClientRef.current = queryClient;

  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Clean up debounce timer on unmount
  useEffect(() => {
    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, []);

  const save = useCallback(
    (
      entriesToUpdate: { name: string; value: string }[],
      entriesToRemove: string[],
    ) => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }

      debounceTimerRef.current = setTimeout(() => {
        const onSuccess = () => {
          invalidateAllListEnvironments(queryClientRef.current);
        };
        const onError = () => {
          toast.error("Failed to save credentials");
        };

        if (!existingEnvironmentRef.current) {
          createMutationRef.current.mutate(
            {
              request: {
                createEnvironmentForm: {
                  name: slugRef.current,
                  organizationId: orgIdRef.current,
                  entries: entriesToUpdate,
                },
              },
            },
            { onSuccess, onError },
          );
        } else {
          updateMutationRef.current.mutate(
            {
              request: {
                slug: slugRef.current,
                updateEnvironmentRequestBody: {
                  entriesToUpdate,
                  entriesToRemove,
                },
              },
            },
            { onSuccess, onError },
          );
        }
      }, 1000);
    },
    [],
  );

  return {
    slug,
    exists: !!existingEnvironment,
    storedEntries,
    save,
    isSaving: createMutation.isPending || updateMutation.isPending,
  };
}
