import { invalidateAllSkill } from "@gram/client/react-query/skill.js";
import { invalidateAllSkillDistributions } from "@gram/client/react-query/skillDistributions.js";
import { invalidateAllSkillEfficacyInsights } from "@gram/client/react-query/skillEfficacyInsights.js";
import { invalidateAllSkillFeedback } from "@gram/client/react-query/skillFeedback.js";
import { invalidateAllSkillSuggestions } from "@gram/client/react-query/skillSuggestions.js";
import { invalidateAllSkillTags } from "@gram/client/react-query/skillTags.js";
import { invalidateAllSkillVersions } from "@gram/client/react-query/skillVersions.js";
import { invalidateAllSkills } from "@gram/client/react-query/skills.js";
import type { QueryClient } from "@tanstack/react-query";

export async function invalidateSkillQueries(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllSkills(queryClient),
    invalidateAllSkill(queryClient),
    invalidateAllSkillDistributions(queryClient),
    invalidateAllSkillVersions(queryClient),
    invalidateAllSkillSuggestions(queryClient),
    invalidateAllSkillFeedback(queryClient),
    invalidateAllSkillEfficacyInsights(queryClient),
    invalidateAllSkillTags(queryClient),
  ]);
}
