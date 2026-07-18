export const ASSISTANT_SKILL_SOFT_CAP = 20;

export function shouldWarnAboutSkillIndex(skillCount: number): boolean {
  return skillCount > ASSISTANT_SKILL_SOFT_CAP;
}
