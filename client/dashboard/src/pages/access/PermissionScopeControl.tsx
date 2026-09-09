import { InlineChoice } from "@/components/access/AccessListRow";
import { Text } from "@/components/ui/Text";
import { X } from "lucide-react";
import type { JSX } from "react";
import type { ResourceType, ScopeRule } from "./types";

/**
 * What one permission applies to, as a single control.
 *
 * A permission is unrestricted until a rule narrows it, and exceptions are the
 * rare case, so the row shows one sentence — "Applies to: All servers" — and
 * keeps everything else in its menu. The earlier row put a chip, a chip
 * dismiss, an "Except…" link and a row dismiss side by side, where two of the
 * four removed different things.
 */
export function PermissionScopeControl({
  allowRule,
  denyRules,
  allowLabel,
  denyLabel,
  resourceType,
  canAddException,
  disabled,
  onChooseSpecific,
  onResetToAll,
  onAddException,
  onEditException,
  onRemoveException,
}: {
  allowRule: ScopeRule | undefined;
  denyRules: { rule: ScopeRule; index: number }[];
  allowLabel: string;
  /** Label per exception, in the same order as denyRules. */
  denyLabel: (rule: ScopeRule) => string;
  resourceType: ResourceType;
  canAddException: boolean;
  disabled?: boolean;
  onChooseSpecific: () => void;
  onResetToAll: () => void;
  onAddException: () => void;
  onEditException: (index: number) => void;
  onRemoveException: (index: number) => void;
}): JSX.Element | null {
  if (!allowRule) return null;

  const everything = isProjectScoped(resourceType)
    ? "All projects"
    : "All servers";
  const specific = isProjectScoped(resourceType)
    ? "Specific projects…"
    : "Specific servers…";

  return (
    // One sentence: "Applies to All servers except 1 server". The exception is
    // a clause in that sentence, not a second row.
    <>
      <InlineChoice
        lead="Applies to"
        value={allowLabel}
        disabled={disabled}
        options={[
          { label: everything, onSelect: onResetToAll },
          { label: specific, onSelect: onChooseSpecific },
          ...(canAddException
            ? [
                {
                  label: "Add an exception…",
                  onSelect: onAddException,
                  separatorBefore: true,
                },
              ]
            : []),
        ]}
      />

      {denyRules.map(({ rule, index }) => (
        <span key={rule.id} className="flex items-center gap-1">
          <Text muted small>
            except
          </Text>
          <button
            type="button"
            onClick={() => onEditException(index)}
            disabled={disabled}
            className="text-foreground px-1 text-sm underline decoration-dotted underline-offset-4 hover:decoration-solid disabled:cursor-not-allowed"
          >
            {denyLabel(rule)}
          </button>
          <button
            type="button"
            onClick={() => onRemoveException(index)}
            disabled={disabled}
            aria-label="Remove exception"
            className="text-muted-foreground hover:text-foreground disabled:cursor-not-allowed"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </span>
      ))}
    </>
  );
}

function isProjectScoped(resourceType: ResourceType): boolean {
  return resourceType === "project" || resourceType === "skill";
}
