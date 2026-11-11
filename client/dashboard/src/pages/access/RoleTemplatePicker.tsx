import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { JSX } from "react";

import type { RoleTemplate } from "./roleTemplates";

/**
 * Where a new role starts from. Picking a template fills the editor below;
 * the first change after that makes the role its own.
 */
export function RoleTemplatePicker({
  templates,
  selected,
  onPick,
}: {
  templates: RoleTemplate[];
  /** The template the editor still matches, or null once it was changed. */
  selected: string | null;
  onPick: (template: RoleTemplate) => void;
}): JSX.Element {
  const current = templates.find((template) => template.key === selected);
  return (
    <div className="@container flex flex-col gap-2">
      <div className="flex flex-wrap items-baseline gap-x-3">
        <Text variant="body" className="font-medium">
          Create from
        </Text>
        <Text muted small>
          Pick a starting point, then adjust below. The role keeps no link to
          it.
        </Text>
      </div>
      <div className="grid grid-cols-2 gap-2 @2xl:grid-cols-3 @5xl:grid-cols-6">
        {templates.map((template) => (
          <button
            key={template.key}
            type="button"
            aria-pressed={template.key === selected}
            onClick={() => onPick(template)}
            className={cn(
              "bg-card flex min-w-0 cursor-pointer flex-col gap-1 border p-3 text-left",
              template.key === selected
                ? "border-foreground"
                : "border-border hover:border-foreground/40",
            )}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="truncate text-sm font-medium">
                {template.label}
              </span>
              {template.system && (
                <span className="text-eyebrow shrink-0">Built-in</span>
              )}
            </span>
            <span className="text-muted-foreground text-xs">
              {template.hint}
            </span>
          </button>
        ))}
      </div>
      <Text muted small>
        {current
          ? `Permissions match ${current.label}. Changing any permission makes them custom.`
          : "Custom permissions. Not matching a starting point."}
      </Text>
    </div>
  );
}
