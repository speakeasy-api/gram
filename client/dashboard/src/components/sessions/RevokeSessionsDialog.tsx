import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { useRevokeUserSessionMutation } from "@gram/client/react-query";

export function RevokeSessionsDialog({
  sessionIds,
  open,
  onOpenChange,
  onRevoked,
}: {
  sessionIds: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRevoked: () => void;
}): JSX.Element {
  const revoke = useRevokeUserSessionMutation();
  const [isRevoking, setIsRevoking] = useState(false);
  const [isError, setIsError] = useState(false);

  const count = sessionIds.length;

  const handleRevoke = async () => {
    setIsError(false);
    setIsRevoking(true);
    try {
      // Fire all revocations concurrently; surface a single failure rather than
      // leaving the caller guessing which of the batch went through.
      await Promise.all(
        sessionIds.map((id) => revoke.mutateAsync({ request: { id } })),
      );
      onOpenChange(false);
      onRevoked();
    } catch {
      setIsError(true);
    } finally {
      setIsRevoking(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            Revoke {count} session{count === 1 ? "" : "s"}?
          </Dialog.Title>
          <Dialog.Description>
            This immediately invalidates{" "}
            {count === 1 ? "this session" : "these sessions"}. The affected
            clients will need to re-authenticate.
          </Dialog.Description>
        </Dialog.Header>
        {isError && (
          <p className="text-destructive text-sm">
            Some sessions couldn&apos;t be revoked. Please try again.
          </p>
        )}
        <Dialog.Footer>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={isRevoking || count === 0}
            onClick={() => void handleRevoke()}
          >
            {isRevoking ? "Revoking…" : `Revoke ${count}`}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
