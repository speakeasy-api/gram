import { useTelemetry } from "@/contexts/Telemetry";
import { handleAPIError } from "@/lib/errors";
import { useRoutes } from "@/routes";
import { useCloneEnvironmentMutation } from "@gram/client/react-query/cloneEnvironment.js";
import { toast } from "sonner";
import { useEnvironments } from "./useEnvironments";

export type CloneEnvironmentInput = {
  sourceSlug: string;
  newName: string;
  copyValues: boolean;
};

export function useCloneEnvironment({
  onSuccess,
}: { onSuccess?: () => void } = {}): {
  clone: (input: CloneEnvironmentInput) => void;
  isPending: boolean;
} {
  const environments = useEnvironments();
  const telemetry = useTelemetry();
  const routes = useRoutes();

  const mutation = useCloneEnvironmentMutation({
    onSuccess: async (data) => {
      telemetry.capture("environment_event", {
        action: "environment_cloned",
        environment_slug: data.slug,
      });
      toast.success(`Environment cloned as "${data.name}"`);
      await environments.refetch();
      routes.environments.environment.goTo(data.slug);
      onSuccess?.();
    },
    onError: (error) => {
      handleAPIError(error, "Failed to clone environment");
    },
  });

  return {
    clone: ({ sourceSlug, newName, copyValues }: CloneEnvironmentInput) =>
      mutation.mutate({
        request: {
          slug: sourceSlug,
          cloneEnvironmentRequestBody: {
            newName,
            copyValues,
          },
        },
      }),
    isPending: mutation.isPending,
  };
}
