import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { subjectLabel } from "@/lib/user-session-status";
import { WorkloadRevocationLadder } from "@/components/sessions/WorkloadSession";
import { workloadIssuerLabel } from "@/lib/workload-session";
import { useRevokeUserSessionMutation } from "@gram/client/react-query/revokeUserSession.js";
import type { UserSession } from "@gram/client/models/components/usersession.js";

export function RevokeSessionDialog({
  session,
  open,
  onOpenChange,
  onRevoked,
}: {
  session: UserSession;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRevoked: () => void;
}): JSX.Element {
  const revoke = useRevokeUserSessionMutation();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Revoke session?</Dialog.Title>
          {session.workload ? (
            <Dialog.Description>
              This immediately invalidates this one session for the workload{" "}
              {session.workload.externalSubject} from{" "}
              {workloadIssuerLabel(session.workload)}. Revoking does not keep
              the workload out: it can exchange a new token and reconnect.
            </Dialog.Description>
          ) : (
            <Dialog.Description>
              This immediately invalidates the session for{" "}
              {subjectLabel(session)}. The client will need to re-authenticate.
            </Dialog.Description>
          )}
        </Dialog.Header>
        {session.workload ? (
          <WorkloadRevocationLadder workload={session.workload} />
        ) : null}
        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={revoke.isPending}
            onClick={() =>
              revoke.mutate(
                { request: { id: session.id } },
                {
                  onSuccess: () => {
                    onOpenChange(false);
                    onRevoked();
                  },
                },
              )
            }
          >
            {revoke.isPending ? "Revoking…" : "Revoke"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
