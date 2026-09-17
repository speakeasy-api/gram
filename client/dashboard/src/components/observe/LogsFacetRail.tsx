import { Checkbox } from "@/components/ui/Checkbox";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import { useMemo, useState, type ReactNode } from "react";

export type FacetValue = {
  value: string;
  label: string;
  /** Omitted where the source has no count to give, rather than faked. */
  count?: number;
  selected: boolean;
  /**
   * A colour mark shown before the label, as a Tailwind background class. The
   * status facet uses it so a value carries the same dot its rows do, rather
   * than making the reader map a word onto a colour they have already learnt.
   */
  dotClassName?: string;
  /** A logo shown before the label, where the value names something with one. */
  icon?: ReactNode;
};

export type FacetGroup = {
  id: string;
  label: string;
  values: FacetValue[];
  /**
   * Whether the group's current selection is the one it ships with rather than
   * one the reader made. A default selection is not a reason to open the group
   * and spend the rail's vertical space on it.
   */
  isDefaultSelection?: boolean;
  /**
   * Whether the values arrive in a deliberate order that must be kept — the
   * type and status enumerations read as a sequence, and alphabetising them
   * would scramble it. Everything else is a list of names, quicker to find by
   * eye in alphabetical order than ranked by volume.
   */
  keepValueOrder?: boolean;
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

/** Values a group shows before it offers the rest. */
const COLLAPSED_FACET_VALUES = 6;

/**
 * Values a group can hold before scrolling for one becomes worse than typing
 * its name. Below this the list is short enough to read.
 */
const FACET_SEARCH_THRESHOLD = 10;

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
  const appliedCount = group.isDefaultSelection ? 0 : selectedCount;
  // A group with something applied opens: its state is the reason the rows
  // below look the way they do, and a reader arriving on a filtered link
  // should see why without hunting. Tracked as an override rather than seeded
  // state because the selection arrives with the options, a fetch after mount.
  const [override, setOverride] = useState<boolean | null>(null);
  const open = override ?? appliedCount > 0;
  const [showAll, setShowAll] = useState(false);
  const [search, setSearch] = useState("");

  const values = useMemo(
    () =>
      group.keepValueOrder
        ? group.values
        : [...group.values].sort((a, b) =>
            a.label.localeCompare(b.label, undefined, { sensitivity: "base" }),
          ),
    [group.values, group.keepValueOrder],
  );

  const searchable = values.length > FACET_SEARCH_THRESHOLD;
  const term = search.trim().toLowerCase();
  // A selected value always survives the search: it is the reason the rows
  // below look the way they do, and hiding the only way to switch it off
  // behind a cleared search box would be a trap.
  const matching =
    searchable && term
      ? values.filter(
          (value) => value.selected || value.label.toLowerCase().includes(term),
        )
      : values;

  const visible = showAll
    ? matching
    : matching.slice(0, COLLAPSED_FACET_VALUES);
  const hidden = matching.length - visible.length;

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
            aria-label={`Reset ${group.label}`}
            className="text-muted-foreground hover:text-foreground shrink-0 text-[11px] lowercase"
          >
            Reset
          </button>
        )}
      </div>

      {open && searchable && (
        <div className="relative mx-3 mb-1">
          <Icon
            name="search"
            aria-hidden
            className="text-muted-foreground/60 pointer-events-none absolute top-1/2 left-2 size-3 -translate-y-1/2"
          />
          <input
            type="text"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={`Filter ${group.label.toLowerCase()}`}
            aria-label={`Filter ${group.label}`}
            // A plain text input, not type="search": the native control paints
            // its own blue clear button, which belongs to no other field on
            // this page.
            className="border-border bg-card placeholder:text-muted-foreground/60 focus-visible:border-foreground/30 h-6 w-full border pr-6 pl-7 text-[12px] outline-none"
          />
          {search && (
            <button
              type="button"
              onClick={() => setSearch("")}
              aria-label={`Clear ${group.label} filter`}
              className="text-muted-foreground/60 hover:text-foreground absolute top-1/2 right-1.5 -translate-y-1/2"
            >
              <Icon name="x" className="size-3" />
            </button>
          )}
        </div>
      )}

      {open && (
        <ul className="flex flex-col pb-1">
          {visible.length === 0 && (
            <li className="text-muted-foreground py-1 pl-3 text-[13px]">
              {term ? "Nothing matches" : "Nothing in this range"}
            </li>
          )}
          {visible.map((value) => (
            <li key={value.value}>
              <label className="hover:bg-muted/60 group/facet flex cursor-pointer items-center gap-1.5 py-1 pr-2 pl-3 text-[13px]">
                <Checkbox
                  checked={value.selected}
                  onCheckedChange={(checked) =>
                    onToggle(group.id, value.value, checked === true)
                  }
                  className="size-3.5 shrink-0"
                />
                {value.icon && (
                  // No fixed slot: sizing it for the largest mark padded the
                  // smaller ones away from their labels. Each group carries one
                  // kind of mark, so its rows still line up with each other.
                  <span className="flex shrink-0 items-center justify-center">
                    {value.icon}
                  </span>
                )}
                {value.dotClassName && (
                  <span
                    aria-hidden
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      value.dotClassName,
                    )}
                  />
                )}
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
                className="text-muted-foreground hover:text-foreground py-1 pl-3 text-[12px]"
              >
                Show {hidden} more
              </button>
            </li>
          )}
          {/* Only where the list was long enough to have been cut: a group
              that always fitted has nothing to collapse back to. */}
          {showAll && group.values.length > COLLAPSED_FACET_VALUES && (
            <li>
              <button
                type="button"
                onClick={() => setShowAll(false)}
                className="text-muted-foreground hover:text-foreground py-1 pl-3 text-[12px]"
              >
                Show less
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
