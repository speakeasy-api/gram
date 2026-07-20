import type { SkillValidationError } from "@gram/client/models/components/skillvalidationerror.js";

export function SkillValidationErrors({
  errors,
}: {
  errors: SkillValidationError[];
}): JSX.Element {
  return (
    <ul className="text-destructive list-disc space-y-1 pl-5 text-sm">
      {errors.map((error) => (
        <li key={`${error.code}:${error.field}:${error.message}`}>
          <span className="font-mono">{error.field || error.code}</span>:{" "}
          {error.message}
        </li>
      ))}
    </ul>
  );
}
