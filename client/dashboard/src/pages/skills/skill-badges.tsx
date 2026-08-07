import { Badge } from "@/components/ui/Badge";
import {
  SKILL_CLASSIFICATION_OPTIONS,
  SKILL_SOURCE_OPTIONS,
  skillBadgeOption,
  type SkillBadgeOption,
} from "./skill-badge-options";

function labelFor(value: string, options: readonly SkillBadgeOption[]): string {
  const known = skillBadgeOption(options, value);
  if (known) return known.label;
  return value
    .replaceAll("_", " ")
    .replace(/\b\w/g, (character) => character.toUpperCase());
}

export function SkillSourceBadge({ value }: { value: string }): JSX.Element {
  const option = skillBadgeOption(SKILL_SOURCE_OPTIONS, value);
  return (
    <Badge
      variant={value === "captured" ? "information" : "neutral"}
      tooltip={option?.description}
    >
      <Badge.Text>{labelFor(value, SKILL_SOURCE_OPTIONS)}</Badge.Text>
    </Badge>
  );
}

export function SkillClassificationBadge({
  value,
}: {
  value: string;
}): JSX.Element {
  const option = skillBadgeOption(SKILL_CLASSIFICATION_OPTIONS, value);
  return (
    <Badge variant="neutral" tooltip={option?.description}>
      <Badge.Text>{labelFor(value, SKILL_CLASSIFICATION_OPTIONS)}</Badge.Text>
    </Badge>
  );
}
