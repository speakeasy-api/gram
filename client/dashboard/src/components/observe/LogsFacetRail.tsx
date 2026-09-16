import { Checkbox } from "@/components/ui/Checkbox";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import { useState, type ReactNode } from "react";

export type FacetValue = {
  value: string;
  label: string;
  /** Omitted where the source has no count to give, rather than faked. */
  count?: number;
  selected: boolean;
};

export type FacetGroup = {
  id: string;
  label: string;
  values: FacetValue[];
};

/**
 * The filter rail: every dimension's values in one list, with the counts that
 * are actually known.
 *
 * It is a view over the same URL params the toolbar writes, not a second
 * store — every toggle routes back through the same setters, so the two cannot
 * disagree about what is applied.
 */
export function LogsFacetRail({
  groups,
  onToggle,
  onClearGroup,
  header,
  className,
}: {
  groups: FacetGroup[];
  onToggle: (groupId: string, value: string, nextSelected: boolean) => void;
  onClearGroup: (groupId: string) => void;
  /** Rendered above the groups — the window the facets narrow within. */
  header?: ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <div className={cn("flex flex-col gap-0.5 overflow-y-auto", className)}>
      {header && (
        <div className="border-border/60 flex flex-col gap-2 border-b pb-3">
          <span className="text-eyebrow">Time range</span>
          {header}
        </div>
      )}
      {groups.map((group) => (
        <FacetSection
          key={group.id}
          group={group}
          onToggle={onToggle}
          onClearGroup={onClearGroup}
        />
      ))}
    </div>
  );
}

function FacetSection({
  group,
  onToggle,
  onClearGroup,
}: {
  group: FacetGroup;
  onToggle: (groupId: string, value: string, nextSelected: boolean) => void;
  onClearGroup: (groupId: string) => void;
}) {
  const selectedCount = group.values.filter((value) => value.selected).length;
  // A group with something applied opens: its state is the reason the rows
  // below look the way they do, and a reader arriving on a filtered link
  // should see why without hunting. Tracked as an override rather than seeded
  // state because the selection arrives with the options, a fetch after mount.
  const [override, setOverride] = useState<boolean | null>(null);
  const open = override ?? selectedCount > 0;
  const [showAll, setShowAll] = useState(false);

  const visible = showAll ? group.values : group.values.slice(0, 6);
  const hidden = group.values.length - visible.length;

  return (
    <div className="border-border/60 flex flex-col border-b py-1 last:border-b-0">
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={() => setOverride(!open)}
          className="text-foreground hover:text-foreground/80 flex min-w-0 flex-1 items-center gap-1.5 py-1.5 text-left font-mono text-[11px] tracking-wide uppercase"
          aria-expanded={open}
        >
          <Icon
            name={open ? "chevron-down" : "chevron-right"}
            className="text-muted-foreground size-3.5 shrink-0"
          />
          <span className="truncate">{group.label}</span>
          {selectedCount > 0 && (
            <span className="bg-foreground text-background shrink-0 px-1.5 font-mono text-[10px] tabular-nums">
              {selectedCount}
            </span>
          )}
        </button>
        {selectedCount > 0 && (
          <button
            type="button"
            onClick={() => onClearGroup(group.id)}
            className="text-muted-foreground hover:text-foreground shrink-0 text-[11px] lowercase"
          >
            Reset
          </button>
        )}
      </div>

      {open && (
        <ul className="flex flex-col pb-1">
          {visible.length === 0 && (
            <li className="text-muted-foreground py-1 pl-6 text-[13px]">
              Nothing in this range
            </li>
          )}
          {visible.map((value) => (
            <li key={value.value}>
              <label className="hover:bg-muted/60 group/facet flex cursor-pointer items-center gap-2.5 py-1 pr-2 pl-6 text-[13px]">
                <Checkbox
                  checked={value.selected}
                  onCheckedChange={(checked) =>
                    onToggle(group.id, value.value, checked === true)
                  }
                  className="size-3.5 shrink-0"
                />
                <span
                  className={cn(
                    "min-w-0 flex-1 truncate",
                    value.selected
                      ? "text-foreground"
                      : "text-muted-foreground",
                  )}
                  title={value.label}
                >
                  {value.label}
                </span>
                {value.count !== undefined && (
                  <span className="text-muted-foreground shrink-0 font-mono text-[11px] tabular-nums">
                    {value.count.toLocaleString()}
                  </span>
                )}
              </label>
            </li>
          ))}
          {hidden > 0 && (
            <li>
              <button
                type="button"
                onClick={() => setShowAll(true)}
                className="text-muted-foreground hover:text-foreground py-1 pl-6 text-[12px]"
              >
                Show {hidden} more
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
