import { InputField } from "@/components/moon/input-field";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import { MultiSelect } from "@/components/ui/MultiSelect";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useSkillTags } from "@gram/client/react-query/skillTags.js";
import { useUpdateSkillMutation } from "@gram/client/react-query/updateSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";

const MAX_SKILL_TAGS = 40;

function sameTags(left: string[], right: string[]): boolean {
  if (left.length !== right.length) return false;
  return left.every((tag, index) => tag === right[index]);
}

export function EditSkillDetailsDialog({
  skill,
  open,
  onOpenChange,
}: {
  skill: Skill;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const update = useUpdateSkillMutation();
  const tagsQuery = useSkillTags(undefined, undefined, { throwOnError: false });
  const [name, setName] = useState(skill.name);
  const [displayName, setDisplayName] = useState(skill.displayName);
  const [summary, setSummary] = useState(skill.summary ?? "");
  const [tags, setTags] = useState(() => [...skill.tags]);
  const [error, setError] = useState<string | null>(null);
  const trimmedName = name.trim();
  const trimmedDisplayName = displayName.trim();
  const trimmedSummary = summary.trim();
  const nameTooLong = trimmedName.length > 64;
  const displayNameTooLong = Array.from(trimmedDisplayName).length > 256;
  const summaryTooLong = Array.from(trimmedSummary).length > 1024;
  const tooManyTags = tags.length > MAX_SKILL_TAGS;
  const unchanged =
    trimmedName === skill.name &&
    trimmedDisplayName === skill.displayName &&
    trimmedSummary === (skill.summary ?? "") &&
    sameTags(tags, skill.tags);
  const tagOptions = [
    ...new Set([...(tagsQuery.data?.tags ?? []), ...tags]),
  ].map((tag) => ({ label: tag, value: tag }));

  const handleOpenChange = (nextOpen: boolean): void => {
    if (!nextOpen && update.isPending) return;
    onOpenChange(nextOpen);
  };

  const save = async (): Promise<void> => {
    setError(null);
    try {
      await update.mutateAsync({
        request: {
          updateSkillRequestBody: {
            id: skill.id,
            name: trimmedName,
            displayName: trimmedDisplayName,
            summary: trimmedSummary || undefined,
            tags,
          },
        },
      });
      await invalidateSkillQueries(queryClient);
      onOpenChange(false);
      toast.success("Skill details updated");
    } catch (updateError) {
      const message =
        updateError instanceof Error
          ? updateError.message
          : "Unable to update skill details.";
      setError(message);
      toast.error("Unable to update skill details");
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Edit skill details</Dialog.Title>
          <Dialog.Description>
            Rename the registry entry without changing existing local copies.
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-4">
          <InputField
            label="Canonical name"
            hint="Unique within this project."
            error={
              nameTooLong
                ? "Canonical name must be 64 characters or fewer."
                : undefined
            }
            value={name}
            disabled={update.isPending}
            onChange={(event) => setName(event.currentTarget.value)}
          />
          <InputField
            label="Display name"
            error={
              displayNameTooLong
                ? "Display name must be 256 characters or fewer."
                : undefined
            }
            value={displayName}
            disabled={update.isPending}
            onChange={(event) => setDisplayName(event.currentTarget.value)}
          />
          <InputField
            label="Summary"
            error={
              summaryTooLong
                ? "Summary must be 1,024 characters or fewer."
                : undefined
            }
            value={summary}
            disabled={update.isPending}
            onChange={(event) => setSummary(event.currentTarget.value)}
          />
          <div className="space-y-2">
            <Label htmlFor="skill-tags" className="text-sm font-medium">
              Tags
            </Label>
            <MultiSelect
              id="skill-tags"
              options={tagOptions}
              defaultValue={tags}
              onValueChange={setTags}
              placeholder="Add tags..."
              creatable
              searchable
              hideSelectAll
              disabled={update.isPending}
            />
            {tooManyTags ? (
              <p className="text-destructive text-xs">
                Skills may have at most {MAX_SKILL_TAGS} tags.
              </p>
            ) : (
              <p className="text-muted-foreground text-xs">
                Use tags to categorize skills and filter the Skills list.
              </p>
            )}
          </div>
          {error && <ErrorAlert title="Update failed" error={error} />}
        </div>
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={update.isPending}
            onClick={() => handleOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            disabled={
              update.isPending ||
              unchanged ||
              trimmedName.length === 0 ||
              nameTooLong ||
              trimmedDisplayName.length === 0 ||
              displayNameTooLong ||
              summaryTooLong ||
              tooManyTags
            }
            onClick={() => void save()}
          >
            {update.isPending ? "Saving..." : "Save"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
