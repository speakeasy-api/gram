import { invalidateAllSkill } from "@gram/client/react-query/skill.js";
import { invalidateAllSkillVersions } from "@gram/client/react-query/skillVersions.js";
import { invalidateAllSkills } from "@gram/client/react-query/skills.js";
import type { QueryClient } from "@tanstack/react-query";

export async function invalidateSkillQueries(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllSkills(queryClient),
    invalidateAllSkill(queryClient),
    invalidateAllSkillVersions(queryClient),
  ]);
}
