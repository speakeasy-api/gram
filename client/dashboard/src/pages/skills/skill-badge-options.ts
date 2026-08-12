export const SKILL_SOURCE_OPTIONS = [
  {
    value: "manual",
    label: "Manual",
    description: "Added in Speakeasy by someone on your team.",
  },
  {
    value: "captured",
    label: "Captured",
    description:
      "Automatically recorded when an agent in your organization used the skill.",
  },
] as const;

export const SKILL_CLASSIFICATION_OPTIONS = [
  {
    value: "custom",
    label: "Custom",
    description: "Created or curated for your organization.",
  },
  {
    value: "built_in",
    label: "Built-in",
    description: "Provided by a distributed plugin or platform default.",
  },
] as const;

export type SkillBadgeOption = {
  value: string;
  label: string;
  description?: string;
};

export function skillBadgeOption(
  options: readonly SkillBadgeOption[],
  value: string,
): SkillBadgeOption | undefined {
  return options.find((option) => option.value === value);
}
