import type { Skill } from "@gram/client/models/components/skill.js";

export function filterSkills(
  skills: Skill[],
  search: string,
  sourceKinds: string[],
  classifications: string[],
): Skill[] {
  const normalizedSearch = search.trim().toLowerCase();
  return skills.filter((skill) => {
    const searchable =
      `${skill.displayName} ${skill.name} ${skill.summary ?? ""}`.toLowerCase();
    const matchesSearch =
      normalizedSearch.length === 0 || searchable.includes(normalizedSearch);
    const matchesSource =
      sourceKinds.length === 0 || sourceKinds.includes(skill.sourceKind);
    const matchesClassification =
      classifications.length === 0 ||
      classifications.includes(skill.classification);
    return matchesSearch && matchesSource && matchesClassification;
  });
}

export function skillCountLabel({
  active,
  hasNextPage,
  incomplete,
  loadedCount,
  resultCount,
}: {
  active: boolean;
  hasNextPage: boolean;
  incomplete: boolean;
  loadedCount: number;
  resultCount: number;
}): string {
  if (active && incomplete) {
    return `${resultCount} matching loaded`;
  }
  if (hasNextPage) {
    return active ? `Searching ${loadedCount} loaded` : `${loadedCount} loaded`;
  }
  return `${resultCount} skill${resultCount === 1 ? "" : "s"}`;
}
