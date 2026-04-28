import { useTelemetry } from "@/contexts/Telemetry";
import { handleAPIError } from "@/lib/errors";
import { useRoutes } from "@/routes";
import {
  useCloneToolsetMutation,
  useDeleteToolsetMutation,
} from "@gram/client/react-query/index.js";
import { toast } from "sonner";
import { useToolsets } from "./useToolsets";

export function useDeleteToolset({
  onSuccess,
}: { onSuccess?: () => void } = {}) {
  const toolsets = useToolsets();
  const telemetry = useTelemetry();

  const mutation = useDeleteToolsetMutation({
    onSuccess: async () => {
      telemetry.capture("toolset_event", {
        action: "toolset_deleted",
      });
      await toolsets.refetch();
      onSuccess?.();
    },
    onError: (error) => {
      handleAPIError(error, "Failed to delete toolset");
    },
  });

  return (slug: string) => {
    if (
      confirm(
        "Are you sure you want to delete this toolset? This action cannot be undone.",
      )
    ) {
      mutation.mutate({
        request: {
          slug,
        },
      });
    }
  };
}

export function useCloneToolset({
  onSuccess,
}: { onSuccess?: () => void } = {}) {
  const toolsets = useToolsets();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const mutation = useCloneToolsetMutation({
    onSuccess: async (data) => {
      telemetry.capture("toolset_event", {
        action: "toolset_cloned",
        toolset_slug: data.slug,
      });
      toast.success(`Toolset cloned successfully as "${data.name}"`);
      await toolsets.refetch();
      routes.mcp.details.goTo(data.slug);
      onSuccess?.();
    },
    onError: (error) => {
      handleAPIError(error, "Failed to clone toolset");
    },
  });

  return (slug: string) => {
    mutation.mutate({
      request: {
        slug,
      },
    });
  };
}
