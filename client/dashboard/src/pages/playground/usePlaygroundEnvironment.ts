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

/**
 * Mirror of server-side `conv.ToSlug` (server/internal/conv/from.go).
 * Strips characters not in [a-zA-Z0-9\s-], lowercases, collapses runs of
 * dashes/whitespace into a single dash, and trims leading/trailing dashes.
 * Keep this in sync with the server implementation.
 */
function toServerSlug(s: string): string {
  return s
    .replace(/[^a-zA-Z0-9\s-]/g, "")
    .toLowerCase()
    .replace(/[-\s]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export interface UsePlaygroundEnvironmentReturn {
  slug: string;
  exists: boolean;
  storedEntries: { name: string; hasStoredValue: boolean }[];
  save: (
    entriesToUpdate: { name: string; value: string }[],
    entriesToRemove: string[],
  ) => Promise<void>;
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
  // We normalize on the client to match the server's transformation
  // (server/internal/conv/from.go:163), otherwise IDs containing
  // characters like underscores would diverge between client and server.
  const slug = toServerSlug(`playground-${user.id}-${toolset.slug}`);

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
    async (
      entriesToUpdate: { name: string; value: string }[],
      entriesToRemove: string[],
    ): Promise<void> => {
      try {
        if (!existingEnvironment) {
          await createMutation.mutateAsync({
            request: {
              createEnvironmentForm: {
                name: slug,
                organizationId: organization.id,
                entries: entriesToUpdate,
              },
            },
          });
        } else {
          await updateMutation.mutateAsync({
            request: {
              slug,
              updateEnvironmentRequestBody: {
                entriesToUpdate,
                entriesToRemove,
              },
            },
          });
        }
        invalidateAllListEnvironments(queryClient);
      } catch (err) {
        toast.error("Failed to save credentials");
        throw err;
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
