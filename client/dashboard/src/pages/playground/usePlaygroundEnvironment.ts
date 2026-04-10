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

export interface SaveResult {
  /** True if a new environment was created by this save. */
  created: boolean;
  /** True if the save was skipped (nothing meaningful to persist). */
  skipped: boolean;
}

export interface UsePlaygroundEnvironmentReturn {
  slug: string;
  exists: boolean;
  storedEntries: { name: string; hasStoredValue: boolean }[];
  save: (
    entriesToUpdate: { name: string; value: string }[],
    entriesToRemove: string[],
  ) => Promise<SaveResult>;
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
  // Use a short prefix ("pg-") and only the first 8 hex chars of the
  // user ID to stay within the 40-character slug limit enforced by the
  // server (server/design/shared/datatypes.go).
  const slug = toServerSlug(`pg-${user.id.slice(0, 8)}-${toolset.slug}`).slice(
    0,
    40,
  );

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
    ): Promise<SaveResult> => {
      // If nothing meaningful to persist, skip without calling the API.
      // This protects against server edge-cases with empty-payload updates.
      if (entriesToUpdate.length === 0 && entriesToRemove.length === 0) {
        return { created: false, skipped: true };
      }
      try {
        if (!existingEnvironment) {
          // Don't create an empty environment when the user only cleared
          // fields (entriesToRemove is meaningless without an existing env).
          if (entriesToUpdate.length === 0) {
            return { created: false, skipped: true };
          }
          await createMutation.mutateAsync({
            request: {
              createEnvironmentForm: {
                name: slug,
                organizationId: organization.id,
                entries: entriesToUpdate,
              },
            },
          });
          invalidateAllListEnvironments(queryClient);
          return { created: true, skipped: false };
        }
        await updateMutation.mutateAsync({
          request: {
            slug,
            updateEnvironmentRequestBody: {
              entriesToUpdate,
              entriesToRemove,
            },
          },
        });
        invalidateAllListEnvironments(queryClient);
        return { created: false, skipped: false };
      } catch (err) {
        const message = err instanceof Error ? err.message : "Unknown error";
        toast.error(`Failed to save credentials: ${message}`);
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
