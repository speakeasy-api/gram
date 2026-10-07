import { MultiSelect } from "@/components/ui/MultiSelect";
import { useMemo } from "react";

// Enough that a typical selection shows whole before it collapses to a count.
const SCOPE_BADGE_LIMIT = 8;

/** A scope picker: offered scopes to pick, or typed ones to add. */
export function ScopeMultiSelect({
  id,
  labelId,
  options,
  value,
  onValueChange,
  placeholder,
  disabled,
}: {
  id: string;
  labelId: string;
  options: string[];
  value: string[];
  onValueChange: (values: string[]) => void;
  placeholder: string;
  disabled: boolean;
}): JSX.Element {
  // Typed scopes join the list so the menu shows every selection.
  const items = useMemo(
    () =>
      [...new Set([...options, ...value])].map((scope) => ({
        label: scope,
        value: scope,
      })),
    [options, value],
  );
  return (
    <MultiSelect
      id={id}
      // The trigger's built-in aria-label would otherwise hide the label.
      aria-labelledby={labelId}
      options={items}
      value={value}
      onValueChange={onValueChange}
      placeholder={placeholder}
      emptyIndicator="Type a scope to add it."
      // Scopes are case-sensitive, so the badge must not uppercase them.
      badgeClassName="normal-case tracking-normal"
      maxCount={SCOPE_BADGE_LIMIT}
      disabled={disabled}
      creatable
      caseSensitiveCreate
      hideSelectAll
    />
  );
}
