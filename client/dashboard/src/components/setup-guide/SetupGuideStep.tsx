import { SetupGuideAction } from "@/components/setup-guide/SetupGuideLink";
import type { SetupGuideMatch } from "@/components/setup-guide/useSetupGuide";
import { setupGuideProviderName } from "@/components/setup-guide/guideDocs";
import { Button } from "@/components/ui/Button";
import { invalidateMcpServerQueries } from "@/lib/mcp-server-visibility";
import { useSetMcpServerSetupGuideCompletedMutation } from "@gram/client/react-query/setMcpServerSetupGuideCompleted.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

/**
 * Marks a server's upstream setup done, or open again. The flag is the
 * team's, so it lives on the server rather than in this browser.
 */
function useSetupGuideCompleted(serverId: string): {
  setCompleted: (completed: boolean) => void;
  pending: boolean;
} {
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
      toast.error("Couldn't save the setup step. Try again.");
    },
  });
  return {
    setCompleted: (completed) =>
      mutation.mutate({
        request: {
          setSetupGuideCompletedRequestBody: { id: serverId, completed },
        },
      }),
    pending: mutation.isPending,
  };
}

/**
 * The step's buttons: read the guide, then mark it done; once done, a way to
 * open it again in case the upstream changes.
 */
export function SetupGuideStepActions({
  guide,
  serverId,
  done,
  canWrite,
}: {
  guide: SetupGuideMatch;
  serverId: string;
  done: boolean;
  canWrite: boolean;
}): JSX.Element {
  const { setCompleted, pending } = useSetupGuideCompleted(serverId);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <SetupGuideAction guide={guide} variant="secondary" />
      {canWrite ? (
        done ? (
          <Button
            variant="tertiary"
            size="xs"
            disabled={pending}
            onClick={() => setCompleted(false)}
          >
            <Button.Text>Mark not done</Button.Text>
          </Button>
        ) : (
          <Button
            variant="secondary"
            size="xs"
            disabled={pending}
            onClick={() => setCompleted(true)}
          >
            <Button.Text>{pending ? "Saving…" : "Mark done"}</Button.Text>
          </Button>
        )
      ) : null}
    </div>
  );
}

/**
 * Overview's card for the upstream steps, shown once Speakeasy itself is set
 * up: the guide's steps happen in the provider's own console, so it is the
 * next thing to do, not the first.
 */
export function SetupGuideOverviewCard({
  guide,
  serverId,
  canWrite,
}: {
  guide: SetupGuideMatch;
  serverId: string;
  canWrite: boolean;
}): JSX.Element {
  const name = setupGuideProviderName(guide);
  return (
    <div className="bg-card flex flex-wrap items-center justify-between gap-4 border px-5 py-4">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">Next: set up {name}</span>
        <span className="text-muted-foreground text-sm">
          A few steps happen in {name}, not in Speakeasy. Follow the guide, then
          mark it done.
        </span>
      </div>
      <SetupGuideStepActions
        guide={guide}
        serverId={serverId}
        done={false}
        canWrite={canWrite}
      />
    </div>
  );
}
