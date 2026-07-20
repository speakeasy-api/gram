import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, ErrorAlert } from "@/components/ui/alert";
import { Dialog } from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import type { AssistantSkillRef } from "@gram/client/models/components/assistantskillref.js";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useDistributeSkillMutation } from "@gram/client/react-query/distributeSkill.js";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Badge, Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { shouldWarnAboutSkillIndex } from "./assistant-skill-limit";
import { useAssistantDraft } from "./useAssistantDraft";

export function AssistantSkillsSection(): JSX.Element {
  const draft = useAssistantDraft();
  const project = useProject();
  const [addOpen, setAddOpen] = useState(false);
  const attached = draft.assistant?.skills ?? [];
  const shouldLoadSkills = addOpen || attached.length > 0;
  const skillsQuery = useSkillsInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
    enabled: shouldLoadSkills,
  });
  useDrainInfiniteQuery(skillsQuery, shouldLoadSkills);

  const skills = useMemo(
    () => skillsQuery.data?.pages.flatMap((page) => page.result.skills) ?? [],
    [skillsQuery.data?.pages],
  );
  const skillsById = useMemo(
    () => new Map(skills.map((skill) => [skill.id, skill])),
    [skills],
  );
  const attachedIds = new Set(attached.map((ref) => ref.skillId));
  const available = skills.filter((skill) => !attachedIds.has(skill.id));
  const distribute = useDistributeSkillMutation();
  const undistribute = useUndistributeSkillMutation();

  const refresh = async (): Promise<void> => {
    draft.invalidateSkillAttachments();
    await draft.refetchAssistant();
  };

  const addSkill: React.FormEventHandler<HTMLFormElement> = (event) => {
    event.preventDefault();
    if (!draft.assistantId) return;
    const skillId = new FormData(event.currentTarget).get("skillId");
    if (typeof skillId !== "string" || !skillId) return;
    distribute.mutate(
      {
        request: {
          distributeSkillRequestBody: {
            id: skillId,
            assistantId: draft.assistantId,
          },
        },
      },
      {
        onSuccess: () => {
          setAddOpen(false);
          void refresh()
            .then(() => toast.success("Skill attached"))
            .catch(() => {
              draft.invalidateAll();
              toast.error("Skill attached, but the assistant did not refresh");
            });
        },
        onError: () => {
          toast.error("Unable to attach skill");
        },
      },
    );
  };

  const updatePin = (skillId: string, pinnedVersionId?: string) => {
    if (!draft.assistantId) return;
    distribute.mutate(
      {
        request: {
          distributeSkillRequestBody: {
            id: skillId,
            assistantId: draft.assistantId,
            ...(pinnedVersionId ? { pinnedVersionId } : {}),
          },
        },
      },
      {
        onSuccess: () => {
          void refresh().catch(() => {
            draft.invalidateAll();
            toast.error("Version updated, but the assistant did not refresh");
          });
        },
        onError: () => {
          toast.error("Unable to update skill version");
        },
      },
    );
  };

  const removeSkill = (skillId: string) => {
    if (!draft.assistantId) return;
    undistribute.mutate(
      {
        request: {
          undistributeSkillRequestBody: {
            id: skillId,
            assistantId: draft.assistantId,
          },
        },
      },
      {
        onSuccess: () => {
          void refresh()
            .then(() => toast.success("Skill detached"))
            .catch(() => {
              draft.invalidateAll();
              toast.error("Skill detached, but the assistant did not refresh");
            });
        },
        onError: () => {
          toast.error("Unable to detach skill");
        },
      },
    );
  };

  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <Type variant="body" className="text-xs font-semibold uppercase">
          Skills ({attached.length})
        </Type>
        <RequireScope
          scope={["skill:read", "project:write"]}
          all
          resourceId={project.id}
          level="component"
          reason="You need skill read and project write access to attach skills."
        >
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setAddOpen(true)}
            disabled={skillsQuery.hasNextPage}
          >
            <Button.LeftIcon>
              <Icon name="plus" className="h-3 w-3" />
            </Button.LeftIcon>
            <Button.Text>Add</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {shouldWarnAboutSkillIndex(attached.length) && (
        <Alert variant="warning" className="mb-3 px-3 py-2">
          <AlertDescription>
            With this many skills, the skill index adds roughly 2k tokens to
            every thread.
          </AlertDescription>
        </Alert>
      )}

      {attached.length === 0 ? (
        <Type small muted>
          No skills attached.
        </Type>
      ) : (
        <Stack gap={2}>
          {attached.map((ref) => (
            <AttachedSkillRow
              key={ref.skillId}
              skillRef={ref}
              skill={skillsById.get(ref.skillId)}
              disabled={distribute.isPending || undistribute.isPending}
              onPin={(versionId) => updatePin(ref.skillId, versionId)}
              onRemove={() => removeSkill(ref.skillId)}
            />
          ))}
        </Stack>
      )}

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Attach skill</Dialog.Title>
            <Dialog.Description>
              Add a project skill to this assistant. It will track the latest
              valid version by default.
            </Dialog.Description>
          </Dialog.Header>
          <form className="flex flex-col gap-4" onSubmit={addSkill}>
            {skillsQuery.error && !skillsQuery.data ? (
              <ErrorAlert
                title="Unable to load skills"
                error={skillsQuery.error}
              />
            ) : skillsQuery.isPending || skillsQuery.hasNextPage ? (
              <Skeleton className="h-9 w-full" />
            ) : available.length === 0 ? (
              <Type small muted>
                No skills available to attach.
              </Type>
            ) : (
              <select
                name="skillId"
                required
                className="bg-background rounded-md border px-3 py-2 text-sm"
                aria-label="Skill"
              >
                <option value="">Select a skill</option>
                {available.map((skill) => (
                  <option key={skill.id} value={skill.id}>
                    {skill.displayName}
                  </option>
                ))}
              </select>
            )}
            <Dialog.Footer>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setAddOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={distribute.isPending || available.length === 0}
              >
                Attach
              </Button>
            </Dialog.Footer>
          </form>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function AttachedSkillRow({
  skillRef,
  skill,
  disabled,
  onPin,
  onRemove,
}: {
  skillRef: AssistantSkillRef;
  skill?: Skill;
  disabled: boolean;
  onPin: (versionId?: string) => void;
  onRemove: () => void;
}): JSX.Element {
  const project = useProject();
  const [versionsOpen, setVersionsOpen] = useState(false);
  const versionsQuery = useSkillVersionsInfinite(
    { id: skillRef.skillId, limit: 50 },
    undefined,
    { enabled: !!skill && versionsOpen, throwOnError: false },
  );
  useDrainInfiniteQuery(versionsQuery, !!skill && versionsOpen);
  const versions =
    versionsQuery.data?.pages
      .flatMap((page) => page.result.versions)
      .filter((version) => version.specValid) ?? [];
  const pinnedVersion = versions.find(
    (version) => version.id === skillRef.pinnedVersionId,
  );
  const skillLabel = skill?.displayName ?? `Unknown skill ${skillRef.skillId}`;

  return (
    <div className="border-border rounded-md border px-3 py-2">
      <div className="flex items-start justify-between gap-2">
        <Stack gap={0} className="min-w-0">
          <Type small className="truncate font-medium">
            {skill?.displayName ?? `Unknown skill (${skillRef.skillId})`}
          </Type>
          <Type small muted mono className="truncate text-[11px]">
            {skill?.name ?? skillRef.skillId}
          </Type>
        </Stack>
        <Badge variant={skillRef.pinnedVersionId ? "neutral" : "information"}>
          {skillRef.pinnedVersionId ? "Pinned" : "Latest"}
        </Badge>
      </div>
      <RequireScope
        scope={["skill:read", "project:write"]}
        all
        resourceId={project.id}
        level="component"
        className="mt-2 w-full"
      >
        <div className="flex w-full items-center gap-2">
          {skill ? (
            <Select
              value={skillRef.pinnedVersionId ?? "latest"}
              onOpenChange={setVersionsOpen}
              onValueChange={(value) =>
                onPin(value === "latest" ? undefined : value)
              }
              disabled={disabled}
            >
              <SelectTrigger
                size="sm"
                className="min-w-0 flex-1"
                aria-label={`Version for ${skillLabel}`}
              >
                <SelectValue>
                  {skillRef.pinnedVersionId
                    ? `Pinned ${pinnedVersion?.canonicalSha256.slice(0, 8) ?? skillRef.pinnedVersionId.slice(0, 8)}`
                    : "Latest"}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="latest">Latest</SelectItem>
                {versionsQuery.error && (
                  <SelectItem value="versions-unavailable" disabled>
                    Unable to load versions
                  </SelectItem>
                )}
                {versions.map((version) => (
                  <SelectItem key={version.id} value={version.id}>
                    {version.canonicalSha256.slice(0, 8)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Type small muted className="flex-1">
              Version unavailable
            </Type>
          )}
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={onRemove}
            aria-label={`Remove ${skillLabel}`}
          >
            Remove
          </Button>
        </div>
      </RequireScope>
    </div>
  );
}
