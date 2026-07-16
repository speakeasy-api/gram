import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { useArchiveSkillMutation } from "@gram/client/react-query/archiveSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";

export interface ArchiveSkillTarget {
  id: string;
  displayName: string;
}

export function ArchiveSkillDialog({
  skill,
  onClose,
  onArchived,
}: {
  skill: ArchiveSkillTarget | null;
  onClose: () => void;
  onArchived?: (skill: ArchiveSkillTarget) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const archive = useArchiveSkillMutation();
  const [error, setError] = useState<string | null>(null);

  const archiveSkill = async (target: ArchiveSkillTarget): Promise<void> => {
    setError(null);
    try {
      await archive.mutateAsync({
        request: { archiveSkillRequestBody: { id: target.id } },
      });
      onClose();
      onArchived?.(target);
      await invalidateSkillQueries(queryClient);
      toast.success(`${target.displayName} archived`);
    } catch (archiveError) {
      let message = "Unable to archive skill.";
      if (archiveError instanceof Error) message = archiveError.message;
      setError(message);
      toast.error("Unable to archive skill");
    }
  };

  return (
    <Dialog
      open={skill !== null}
      onOpenChange={(open) => {
        if (!open) {
          setError(null);
          onClose();
        }
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Archive {skill?.displayName}?</Dialog.Title>
          <Dialog.Description>
            Archiving removes the skill from the active project registry. Its
            recorded versions are kept.
          </Dialog.Description>
        </Dialog.Header>
        {error && <ErrorAlert title="Archive failed" error={error} />}
        <Dialog.Footer>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={archive.isPending || skill === null}
            onClick={() => {
              if (skill) void archiveSkill(skill);
            }}
          >
            {archive.isPending ? "Archiving..." : "Archive skill"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
