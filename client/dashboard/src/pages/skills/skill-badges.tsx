import { Badge } from "@speakeasy-api/moonshine";
import {
  SKILL_CLASSIFICATION_OPTIONS,
  SKILL_SOURCE_OPTIONS,
} from "./skill-badge-options";

function labelFor(value: string): string {
  const known = [...SKILL_SOURCE_OPTIONS, ...SKILL_CLASSIFICATION_OPTIONS].find(
    (option) => option.value === value,
  );
  if (known) return known.label;
  return value
    .replaceAll("_", " ")
    .replace(/\b\w/g, (character) => character.toUpperCase());
}

export function SkillSourceBadge({ value }: { value: string }): JSX.Element {
  return (
    <Badge variant={value === "captured" ? "information" : "neutral"}>
      {labelFor(value)}
    </Badge>
  );
}

export function SkillClassificationBadge({
  value,
}: {
  value: string;
}): JSX.Element {
  return <Badge variant="neutral">{labelFor(value)}</Badge>;
}
