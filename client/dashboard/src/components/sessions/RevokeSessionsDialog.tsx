import { useEffect } from "react";
import { Link } from "react-router";
import { useMutation } from "@tanstack/react-query";

import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { useRevokeUserSessionMutation } from "@gram/client/react-query/revokeUserSession.js";

export function RevokeSessionsDialog({
  sessionIds,
  open,
  onOpenChange,
  onRevoked,
  newKillswitchHref,
}: {
  sessionIds: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Only supplied when the group identifies one exact canonical user. */
  newKillswitchHref?: string;
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
            This immediately ends{" "}
            {count === 1 ? "this session" : "these sessions"}, but clients can
            authenticate and reconnect. A killswitch is separate: it blocks
            matching MCP tool calls without ending sessions. Revocation never
            creates or lifts a killswitch.
          </Dialog.Description>
        </Dialog.Header>
        {newKillswitchHref && (
          <div className="space-y-1">
            <Button variant="secondary" size="sm" asChild>
              <Link to={newKillswitchHref}>New killswitch…</Link>
            </Button>
            <p className="text-muted-foreground text-xs">
              This separate action blocks matching tool calls; it does not
              revoke or prevent connections.
            </p>
          </div>
        )}
        {failedCount > 0 && (
          <p className="text-destructive text-sm">
            {failedCount} session{failedCount === 1 ? "" : "s"} couldn&apos;t be
            revoked. Please try again.
          </p>
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
