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
import { Textarea } from "@/components/ui/textarea";
import { useCreateReleaseMutation } from "@gram/client/react-query";
import { useState, useCallback } from "react";
import { toast } from "sonner";

interface CreateReleaseDialogProps {
  toolsetSlug: string;
  onReleaseCreated?: () => void;
}

export const CreateReleaseDialog = ({
  toolsetSlug,
  onReleaseCreated,
}: CreateReleaseDialogProps) => {
  const [open, setOpen] = useState(false);
  const [notes, setNotes] = useState("");
  const createRelease = useCreateReleaseMutation();

  const handleCreate = useCallback(async () => {
    try {
      const result = await createRelease.mutateAsync({
        request: {
          toolsetSlug,
          notes: notes.trim() || undefined,
        },
      });

      toast.success("Release created successfully", {
        description: `Release #${result.releaseNumber} has been published`,
      });

      setOpen(false);
      setNotes("");
      onReleaseCreated?.();
    } catch (error) {
      toast.error("Failed to create release", {
        description: error instanceof Error ? error.message : "Unknown error",
      });
    }
  }, [toolsetSlug, notes, createRelease, onReleaseCreated]);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="primary" size="small">
          <Icon name="rocket" size="small" />
          Create Release
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Release</DialogTitle>
          <DialogDescription>
            Create a new release from the current staging toolset state. This
            will publish all changes made since the last release.
          </DialogDescription>
        </DialogHeader>

        <Stack gap={4} className="py-4">
          <Stack gap={2}>
            <label htmlFor="notes" className="text-sm font-medium">
              Release Notes (optional)
            </label>
            <Textarea
              id="notes"
              placeholder="Describe what changed in this release..."
              value={notes}
              onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                setNotes(e.target.value)
              }
              rows={4}
            />
          </Stack>
        </Stack>

        <DialogFooter>
          <Button
            variant="secondary"
            onClick={() => setOpen(false)}
            disabled={createRelease.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={handleCreate}
            disabled={createRelease.isPending}
          >
            {createRelease.isPending ? "Creating..." : "Create Release"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
