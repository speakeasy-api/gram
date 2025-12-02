import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useRollbackToReleaseMutation } from "@gram/client/react-query";
import { useState, useCallback } from "react";
import { toast } from "sonner";

interface RollbackDialogProps {
  toolsetSlug: string;
  releaseNumber: number;
  onRollbackComplete?: () => void;
}

export const RollbackDialog = ({
  toolsetSlug,
  releaseNumber,
  onRollbackComplete,
}: RollbackDialogProps) => {
  const [open, setOpen] = useState(false);
  const rollback = useRollbackToReleaseMutation();

  const handleRollback = useCallback(async () => {
    try {
      await rollback.mutateAsync({
        request: {
          slug: toolsetSlug,
          releaseNumber,
        },
      });

      toast.success("Rollback successful", {
        description: `Rolled back to release #${releaseNumber}`,
      });

      setOpen(false);
      onRollbackComplete?.();
    } catch (error) {
      toast.error("Rollback failed", {
        description: error instanceof Error ? error.message : "Unknown error",
      });
    }
  }, [toolsetSlug, releaseNumber, rollback, onRollbackComplete]);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="secondary" size="small">
          <Icon name="rotate-ccw" size="small" />
          Rollback
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Rollback to Release #{releaseNumber}</DialogTitle>
          <DialogDescription>
            This will restore your toolset to the state it was in at release #
            {releaseNumber}. A new version will be created with the state from
            this release.
          </DialogDescription>
        </DialogHeader>

        <Stack gap={4} className="py-4">
          <Stack
            gap={2}
            className="p-4 rounded-lg bg-warning/10 border border-warning/20"
          >
            <Stack direction="horizontal" gap={2} align="center">
              <Icon
                name="alert-triangle"
                size="small"
                className="text-warning"
              />
              <Type variant="label" className="text-warning">
                Important
              </Type>
            </Stack>
            <Type variant="body" className="text-sm">
              This action will create a new version with the state from release
              #{releaseNumber}. Your version history will be preserved, but the
              current state will be replaced.
            </Type>
          </Stack>
        </Stack>

        <DialogFooter>
          <Button
            variant="secondary"
            onClick={() => setOpen(false)}
            disabled={rollback.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={handleRollback}
            disabled={rollback.isPending}
          >
            {rollback.isPending ? "Rolling back..." : "Rollback"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
