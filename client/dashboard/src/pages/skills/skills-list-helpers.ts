import type { Skill } from "@gram/client/models/components/skill.js";

export function filterSkills(
  skills: Skill[],
  search: string,
  sourceKinds: string[],
  classifications: string[],
  tags: string[] = [],
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
    const matchesTags =
      tags.length === 0 || tags.some((tag) => skill.tags.includes(tag));
    return (
      matchesSearch && matchesSource && matchesClassification && matchesTags
    );
  });
}

export function prioritizeAddableSkills(skills: Skill[]): Skill[] {
  return skills.toSorted(
    (left, right) =>
      Number(right.hasValidVersion) - Number(left.hasValidVersion),
  );
}
