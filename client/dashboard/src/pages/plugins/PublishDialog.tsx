import { InputField } from "@/components/moon/input-field";
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@speakeasy-api/moonshine";
import { memo, useCallback } from "react";

interface PublishDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPublish: (githubUsername?: string) => void;
  isPending: boolean;
}

export const PublishDialog = memo(function PublishDialog({
  open,
  onOpenChange,
  onPublish,
  isPending,
}: PublishDialogProps) {
  const handleSubmit = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      const fd = new FormData(e.currentTarget);
      const username = (fd.get("githubUsername") as string) || undefined;
      onPublish(username);
      // Dialog close is driven by the parent's mutation onSuccess so the
      // pending state stays visible during the publish.
    },
    [onPublish],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Publish Plugins</Dialog.Title>
          <Dialog.Description>
            Publish all plugins to a GitHub repository. Optionally add a
            collaborator who will receive read access to the repo.
          </Dialog.Description>
          <Dialog.Description>
            At least one user in your organization will need to be given access
            to connect the generated repository with Claude, Cursor, or Codex.
          </Dialog.Description>
        </Dialog.Header>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <InputField
            label="GitHub Username"
            name="githubUsername"
            placeholder="e.g. octocat"
          />
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => onOpenChange(false)}
              type="button"
            >
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? "Publishing..." : "Publish"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
});
