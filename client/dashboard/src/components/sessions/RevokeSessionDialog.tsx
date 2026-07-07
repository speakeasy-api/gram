import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { subjectLabel } from "@/lib/user-session-status";
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
          <Dialog.Description>
            This immediately invalidates the session for {subjectLabel(session)}
            . The client will need to re-authenticate.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
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
