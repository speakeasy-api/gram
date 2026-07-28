import type { Skill } from "@gram/client/models/components/skill.js";
import type { SkillInsightMetrics } from "@gram/client/models/components/skillinsightmetrics.js";

export type SkillSort =
  | "updated"
  | "activations"
  | "efficacy"
  | "estimated_savings";

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

export function sortSkills(
  skills: Skill[],
  metricsBySkill: ReadonlyMap<string, SkillInsightMetrics>,
  sort: SkillSort,
): Skill[] {
  return [...skills].sort((left, right) => {
    let difference = 0;
    switch (sort) {
      case "updated":
        difference = right.updatedAt.getTime() - left.updatedAt.getTime();
        break;
      case "activations":
        difference =
          (metricsBySkill.get(right.id)?.activations ?? 0) -
          (metricsBySkill.get(left.id)?.activations ?? 0);
        break;
      case "efficacy":
        difference =
          (metricsBySkill.get(right.id)?.efficacy?.averageScore ?? -1) -
          (metricsBySkill.get(left.id)?.efficacy?.averageScore ?? -1);
        break;
      case "estimated_savings":
        difference =
          (metricsBySkill.get(right.id)?.efficacy?.estimatedMinutesSavedTotal ??
            -1) -
          (metricsBySkill.get(left.id)?.efficacy?.estimatedMinutesSavedTotal ??
            -1);
        break;
    }
    return difference || left.displayName.localeCompare(right.displayName);
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
