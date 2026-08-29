import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { KillswitchOverlap } from "@gram/client/models/components/killswitchoverlap.js";
import { useEffect, useRef, useState } from "react";
import {
  conflictName,
  newOperationId,
  scheduleLabel,
  scopeLabel,
} from "./killswitch-view-model";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  overlaps: KillswitchOverlap[];
  overlapsTruncated?: boolean;
  serverNames: ReadonlyMap<string, string>;
  previewStatus: "loading" | "ready" | "error";
  previewError?: string;
  onRetryPreview: () => Promise<void>;
  onLift: (operationId: string) => Promise<void>;
};

export function LiftKillswitchDialog({
  open,
  onOpenChange,
  overlaps,
  overlapsTruncated,
  serverNames,
  previewStatus,
  previewError,
  onRetryPreview,
  onLift,
}: Props): JSX.Element {
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<string>();
  const operationId = useRef(newOperationId());

  useEffect(() => {
    if (!open) return;
    operationId.current = newOperationId();
    setError(undefined);
  }, [open]);

  const lift = async () => {
    setIsPending(true);
    setError(undefined);
    try {
      await onLift(operationId.current);
      onOpenChange(false);
    } catch (cause) {
      const conflict = conflictName(cause);
      if (conflict) operationId.current = newOperationId();
      setError(
        conflict === "version_conflict"
          ? "This Killswitch changed. The latest version and overlaps are now shown; review them before lifting again."
          : conflict === "operation_conflict"
            ? "This operation ID was already used. A new ID is ready; review and retry."
            : cause instanceof Error
              ? cause.message
              : "Unable to lift the killswitch.",
      );
    } finally {
      setIsPending(false);
    }
  };

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && isPending) return;
    onOpenChange(nextOpen);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Lift killswitch</Dialog.Title>
          <Dialog.Description>
            Lifting ends this independently managed restriction. It does not
            lift overlapping Killswitches.
          </Dialog.Description>
        </Dialog.Header>
        {previewStatus === "loading" ? (
          <p role="status" className="text-muted-foreground text-sm">
            Refreshing overlaps…
          </p>
        ) : previewStatus === "error" ? (
          <Alert variant="error">
            <AlertTitle>Overlap check failed</AlertTitle>
            <AlertDescription>
              {previewError ?? "Unable to refresh overlaps."}
              <div className="mt-2">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => {
                    void onRetryPreview().catch(() => undefined);
                  }}
                >
                  Try again
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        ) : overlaps.length > 0 ? (
          <Alert variant="warning">
            <AlertTitle>Access may remain blocked</AlertTitle>
            <AlertDescription>
              <ul className="list-disc space-y-1 pl-5">
                {overlaps.map((overlap) => (
                  <li key={overlap.id}>
                    {scopeLabel(overlap.scope, serverNames)} —{" "}
                    {scheduleLabel(overlap.schedule)} ({overlap.status})
                  </li>
                ))}
              </ul>
              {overlapsTruncated && (
                <p>Additional overlapping Killswitches are not shown.</p>
              )}
            </AlertDescription>
          </Alert>
        ) : (
          <p className="text-sm">
            No other overlapping killswitch is currently reported.
          </p>
        )}
        {error && (
          <Alert variant="error">
            <AlertTitle>Lift failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={isPending}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={isPending || previewStatus !== "ready"}
            onClick={() => void lift()}
          >
            {isPending ? "Lifting…" : "Lift killswitch"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
