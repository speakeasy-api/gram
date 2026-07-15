import { useEffect } from "react";
import { useMutation } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useRevokeUserSessionMutation } from "@gram/client/react-query/revokeUserSession.js";

export function RevokeSessionsDialog({
  sessionIds,
  open,
  onOpenChange,
  onRevoked,
}: {
  sessionIds: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Reports the ids that were successfully revoked so the caller can clear them. */
  onRevoked: (succeededIds: string[]) => void;
}): JSX.Element {
  const revoke = useRevokeUserSessionMutation();

  // Revoke each session concurrently. allSettled (not all) means a single
  // failure doesn't discard the sessions that did revoke — we report the
  // successes so the caller clears/refetches them, and keep the failures so the
  // user can retry. react-query owns the pending/result state (no hand-rolled
  // async flags).
  const bulkRevoke = useMutation({
    mutationFn: async (ids: string[]) => {
      const results = await Promise.allSettled(
        ids.map((id) => revoke.mutateAsync({ request: { id } })),
      );
      const succeededIds = ids.filter(
        (_, i) => results[i]?.status === "fulfilled",
      );
      return { succeededIds, failedCount: ids.length - succeededIds.length };
    },
  });

  const { reset, isPending } = bulkRevoke;
  // Clear any prior result when the dialog closes so stale failure messaging
  // doesn't linger across reopens — but not while a batch is still in flight,
  // or the Revoke button could re-enable and allow a duplicate submission.
  // isPending is a dep so the reset still runs once the in-flight batch settles.
  useEffect(() => {
    if (!open && !isPending) reset();
  }, [open, isPending, reset]);

  const count = sessionIds.length;
  const failedCount = bulkRevoke.data?.failedCount ?? 0;

  const handleRevoke = () => {
    bulkRevoke.mutate(sessionIds, {
      onSuccess: (result) => {
        onRevoked(result.succeededIds);
        if (result.failedCount === 0) onOpenChange(false);
      },
    });
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
        {failedCount > 0 && (
          <Type small destructive>
            {failedCount} session{failedCount === 1 ? "" : "s"} couldn&apos;t be
            revoked. Please try again.
          </Type>
        )}
        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={isPending || count === 0}
            onClick={handleRevoke}
          >
            {isPending ? "Revoking…" : `Revoke ${count}`}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
