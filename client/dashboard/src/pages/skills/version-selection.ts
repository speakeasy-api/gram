import type { SkillVersion } from "@gram/client/models/components/skillversion.js";

export function selectDiffVersions(
  versionsNewestFirst: SkillVersion[],
  selectedIds: Set<string>,
  latestVersionId: string,
): [older: SkillVersion, newer: SkillVersion] | null {
  const selected = versionsNewestFirst.filter((version) =>
    selectedIds.has(version.id),
  );
  const selectedVersion = selected[0];
  if (selected.length === 1 && selectedVersion?.id !== latestVersionId) {
    const latest = versionsNewestFirst.find(
      (version) => version.id === latestVersionId,
    );
    return latest && selectedVersion ? [selectedVersion, latest] : null;
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
