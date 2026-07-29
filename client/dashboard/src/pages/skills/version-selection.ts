import type { SkillVersion } from "@gram/client/models/components/skillversion.js";

export type VersionChangeDirection = "backward" | "forward";

export function versionChangeDirection(
  target: SkillVersion,
  current: SkillVersion,
): VersionChangeDirection | null {
  if (target.id === current.id) return null;

  const targetCreatedAt = target.createdAt.getTime();
  const currentCreatedAt = current.createdAt.getTime();
  if (targetCreatedAt === currentCreatedAt) {
    return target.id < current.id ? "backward" : "forward";
  }
  return targetCreatedAt < currentCreatedAt ? "backward" : "forward";
}

export function selectDiffVersions(
  versionsNewestFirst: SkillVersion[],
  selectedIds: Set<string>,
  currentVersionId: string,
): [older: SkillVersion, newer: SkillVersion] | null {
  const selected = versionsNewestFirst.filter((version) =>
    selectedIds.has(version.id),
  );
  const selectedVersion = selected[0];
  if (selected.length === 1 && selectedVersion?.id !== currentVersionId) {
    const current = versionsNewestFirst.find(
      (version) => version.id === currentVersionId,
    );
    if (!current || !selectedVersion) return null;
    const selectedIndex = versionsNewestFirst.indexOf(selectedVersion);
    const currentIndex = versionsNewestFirst.indexOf(current);
    return selectedIndex < currentIndex
      ? [current, selectedVersion]
      : [selectedVersion, current];
  }
  if (selected.length !== 2) return null;

  const newerIndex = versionsNewestFirst.findIndex(
    (version) => version.id === selected[0]!.id,
  );
  const otherIndex = versionsNewestFirst.findIndex(
    (version) => version.id === selected[1]!.id,
  );
  if (newerIndex < otherIndex) return [selected[1]!, selected[0]!];
  return [selected[0]!, selected[1]!];
}
