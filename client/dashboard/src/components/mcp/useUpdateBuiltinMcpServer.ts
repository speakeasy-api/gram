import { useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { invalidateAllBuiltinMcpServers } from "@gram/client/react-query/builtinMcpServers.js";
import { useUpdateBuiltinMcpServerMutation } from "@gram/client/react-query/updateBuiltinMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

type BuiltinMcpUpdateForm = {
  key: string;
  hidden: boolean;
  environmentId?: string;
  expectedVersion: string;
};

export function useUpdateBuiltinMcpServer(): ReturnType<
  typeof useUpdateBuiltinMcpServerMutation
> & { save: (form: BuiltinMcpUpdateForm) => void } {
  const projectSlug = useProjectSlugForRequests();
  const session = useSession();
  const queryClient = useQueryClient();
  const mutation = useUpdateBuiltinMcpServerMutation({
    onSuccess: async () => {
      await invalidateAllBuiltinMcpServers(queryClient);
      toast.success("Built-in MCP settings saved");
    },
    onError: async (error) => {
      toast.error(`Could not save built-in MCP settings: ${error.message}`);
      // Refresh the version after a conflict before offering another write.
      await invalidateAllBuiltinMcpServers(queryClient);
    },
  });
  return {
    ...mutation,
    save: (form: BuiltinMcpUpdateForm): void =>
      mutation.mutate({
        request: { updateBuiltinMcpServerForm: form },
        security: {
          projectSlugHeaderGramProject: projectSlug,
          sessionHeaderGramSession: session.session,
        },
      }),
  };
}
