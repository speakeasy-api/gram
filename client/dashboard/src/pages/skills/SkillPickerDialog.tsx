import { Page } from "@/components/page-layout";
import { ErrorAlert } from "@/components/ui/Alert";
import { Checkbox } from "@/components/ui/Checkbox";
import { Dialog } from "@/components/ui/Dialog";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useRoutes } from "@/routes";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useDistributeSkillMutation } from "@gram/client/react-query/distributeSkill.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import { filterSkills, prioritizeAddableSkills } from "./skills-list-helpers";

type SkillDistributionTarget =
  | { assistantId: string; pluginId?: never }
  | { assistantId?: never; pluginId: string };

export type SkillPickerResult = {
  addedCount: number;
  failedCount: number;
};

export function SkillPickerDialog({
  open,
  onOpenChange,
  excludedSkillIds,
  target,
  title,
  description,
  actionLabel,
  emptyMessage,
  renderSelectionNotice,
  onBatchComplete,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  excludedSkillIds: string[];
  target: SkillDistributionTarget;
  title: string;
  description: string;
  actionLabel: string;
  emptyMessage: string;
  renderSelectionNotice?: (selectedCount: number) => ReactNode;
  onBatchComplete: (result: SkillPickerResult) => void | Promise<void>;
}): JSX.Element {
  const [search, setSearch] = useState("");
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
  const [isBatchAdding, setIsBatchAdding] = useState(false);
  useEffect(() => {
    if (!open) return;
    setSearch("");
    setSelectedSkillIds([]);
  }, [open, target.assistantId, target.pluginId]);
  const skillsQuery = useSkillsInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
    enabled: open,
  });
  useDrainInfiniteQuery(skillsQuery, open);
  const isLoading = skillsQuery.isPending || skillsQuery.hasNextPage;
  const availableSkills = useMemo(() => {
    const excluded = new Set(excludedSkillIds);
    return prioritizeAddableSkills(
      (
        skillsQuery.data?.pages.flatMap((page) => page.result.skills) ?? []
      ).filter((skill) => !excluded.has(skill.id)),
    );
  }, [excludedSkillIds, skillsQuery.data?.pages]);
  const visibleSkills = useMemo(
    () => filterSkills(availableSkills, search, [], []),
    [availableSkills, search],
  );
  const unavailableSkillCount = availableSkills.filter(
    (skill) => !skill.hasValidVersion,
  ).length;
  const hiddenSkillCount = availableSkills.length - visibleSkills.length;
  const listSummary = [
    hiddenSkillCount > 0
      ? `${hiddenSkillCount} skill${hiddenSkillCount === 1 ? "" : "s"} hidden by search`
      : "",
    unavailableSkillCount > 0
      ? `${unavailableSkillCount} unavailable skill${unavailableSkillCount === 1 ? "" : "s"}`
      : "",
  ]
    .filter(Boolean)
    .join(" · ");
  const distribute = useDistributeSkillMutation();

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && isBatchAdding) return;
    onOpenChange(nextOpen);
  };

  const toggleSkill = (skillId: string) => {
    setSelectedSkillIds((current) =>
      current.includes(skillId)
        ? current.filter((id) => id !== skillId)
        : [...current, skillId],
    );
  };

  const handleSubmit = async () => {
    if (selectedSkillIds.length === 0 || isBatchAdding) return;
    setIsBatchAdding(true);
    try {
      const results = await Promise.allSettled(
        selectedSkillIds.map((skillId) =>
          distribute.mutateAsync({
            request: {
              distributeSkillRequestBody: { id: skillId, ...target },
            },
          }),
        ),
      );
      const failedIds = selectedSkillIds.filter(
        (_, index) => results[index]?.status === "rejected",
      );
      const result = {
        addedCount: selectedSkillIds.length - failedIds.length,
        failedCount: failedIds.length,
      };
      setSelectedSkillIds(failedIds);
      await onBatchComplete(result);
      if (failedIds.length === 0) onOpenChange(false);
    } finally {
      setIsBatchAdding(false);
    }
  };

  let pickerContent: JSX.Element;
  if (
    skillsQuery.error &&
    (!skillsQuery.data || skillsQuery.isFetchNextPageError)
  ) {
    pickerContent = (
      <ErrorAlert title="Unable to load skills" error={skillsQuery.error} />
    );
  } else if (isLoading) {
    pickerContent = <Skeleton className="h-24 w-full" />;
  } else if (visibleSkills.length > 0) {
    pickerContent = (
      <div className="flex max-h-64 flex-col gap-0.5 overflow-y-auto border p-1">
        {visibleSkills.map((skill) => (
          <SkillOption
            key={skill.id}
            skill={skill}
            checked={selectedSkillIds.includes(skill.id)}
            disabled={isBatchAdding}
            onToggle={() => toggleSkill(skill.id)}
          />
        ))}
      </div>
    );
  } else if (availableSkills.length > 0) {
    pickerContent = (
      <Text muted small>
        No skills match your search.
      </Text>
    );
  } else {
    pickerContent = (
      <Text muted small>
        {emptyMessage}
      </Text>
    );
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>
        <div className="flex flex-col gap-2">
          <label className="text-sm font-medium">Skills</label>
          <Page.Toolbar.Search
            value={search}
            onChange={setSearch}
            debounceMs={150}
            placeholder="Search skills"
            className="w-full"
          />
          {!isLoading && listSummary && (
            <Text muted small>
              {listSummary}
            </Text>
          )}
          {pickerContent}
          {renderSelectionNotice?.(selectedSkillIds.length)}
        </div>
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={isBatchAdding}
            onClick={() => handleOpenChange(false)}
            type="button"
          >
            Cancel
          </Button>
          <Button
            type="button"
            disabled={
              isBatchAdding || isLoading || selectedSkillIds.length === 0
            }
            onClick={() => void handleSubmit()}
          >
            {selectedSkillIds.length > 1
              ? `${actionLabel} ${selectedSkillIds.length} skills`
              : actionLabel}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function SkillOption({
  skill,
  checked,
  disabled,
  onToggle,
}: {
  skill: Skill;
  checked: boolean;
  disabled: boolean;
  onToggle: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const isDistributable = skill.hasValidVersion;

  return (
    <label
      className={cn(
        "flex items-center gap-2 px-2 py-1.5 text-sm",
        isDistributable ? "hover:bg-accent cursor-pointer" : "opacity-70",
      )}
    >
      <Checkbox
        checked={checked}
        disabled={disabled || !isDistributable}
        onCheckedChange={onToggle}
      />
      <div className="flex min-w-0 flex-col">
        <span className="truncate">{skill.displayName}</span>
        <span className="text-muted-foreground truncate font-mono text-xs">
          {skill.name}
        </span>
        {!isDistributable && (
          <span className="text-muted-foreground text-xs">
            {skill.versionCount === 0
              ? "No versions recorded"
              : "No valid version"}
            {" · "}
            <Link
              to={routes.skills.detail.href(skill.id)}
              className="text-foreground underline underline-offset-2"
            >
              Fix
            </Link>
          </span>
        )}
      </div>
    </label>
  );
}
