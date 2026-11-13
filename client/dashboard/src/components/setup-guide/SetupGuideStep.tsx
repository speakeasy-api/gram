import { Button } from "@/components/ui/Button";
import { invalidateMcpServerQueries } from "@/lib/mcp-server-visibility";
import { useSetMcpServerSetupGuideCompletedMutation } from "@gram/client/react-query/setMcpServerSetupGuideCompleted.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

/**
 * Marks a server's provider setup done, or open again. The flag is the
 * team's, so it lives on the server rather than in this browser.
 */
export function SetupGuideDoneButton({
  serverId,
  done,
}: {
  serverId: string;
  done: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const mutation = useSetMcpServerSetupGuideCompletedMutation({
    onSuccess: async (_, variables) => {
      await invalidateMcpServerQueries(queryClient);
      toast.success(
        variables.request.setSetupGuideCompletedRequestBody.completed
          ? "Setup marked done"
          : "Setup marked not done",
      );
    },
    onError: () => {
      toast.error("Couldn't save the setup status. Try again.");
    },
  });
  const set = (completed: boolean) =>
    mutation.mutate({
      request: {
        setSetupGuideCompletedRequestBody: { id: serverId, completed },
      },
    });
  return done ? (
    <Button
      variant="secondary"
      size="sm"
      disabled={mutation.isPending}
      onClick={() => set(false)}
    >
      <Button.Text>Mark not done</Button.Text>
    </Button>
  ) : (
    <Button
      variant="primary"
      size="sm"
      disabled={mutation.isPending}
      onClick={() => set(true)}
    >
      <Button.Text>{mutation.isPending ? "Saving…" : "Mark done"}</Button.Text>
    </Button>
  );
}
