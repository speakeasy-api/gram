import { Page } from "@/components/page-layout";
import {
  SEGMENT_BASE,
  SEGMENT_INACTIVE,
  SegmentedControl,
} from "@/components/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Info } from "lucide-react";
import type { ReactNode } from "react";

// The header for the breakdown table: what this cut is (title), what it's doing
// to the user's own numbers (caption), and the axes to re-cut it by (the track).
//
// This replaced a bare "Breakdown by <select>": users didn't find the dropdown,
// and the word "breakdown" reads as jargon until you've watched it re-cut the
// same spend. So the axes are promoted to visible segments, and the title states
// the current cut ("Cost by Model") rather than naming the mechanism — pairing a
// lit segment with a title that echoes it teaches the idea on the first click.

export type AxisOption = { value: string; label: string };

// How many axes get promoted into the track. Four keeps the row on one line at
// the narrowest supported width while covering the suggested org chain
// (Division → Department → User → Agent), which is the common path.
const SEGMENT_LIMIT = 4;

/**
 * Split the options into the segments to render inline and the remainder for the
 * "More" select. The active axis is always segmented, even when it sits past the
 * limit — otherwise picking from "More" makes the selection disappear.
 */
function partitionAxes(
  options: AxisOption[],
  activeValue: string,
): { segments: AxisOption[]; overflow: AxisOption[] } {
  const segments = options.slice(0, SEGMENT_LIMIT);
  const overflow = options.slice(SEGMENT_LIMIT);
  const activeIndex = overflow.findIndex((o) => o.value === activeValue);
  if (activeIndex < 0) return { segments, overflow };
  return {
    segments: [...segments, overflow[activeIndex]!],
    overflow: overflow.filter((_, i) => i !== activeIndex),
  };
}

export function BreakdownBar({
  title,
  caption,
  axisValue,
  axisOptions,
  axisHint,
  onAxisChange,
  searchValue,
  onSearchChange,
  searchPlaceholder,
  actions,
}: {
  // The current cut, stated plainly (e.g. "Cost by Model") — the caption's
  // parent, so the prose under it isn't an orphaned third line.
  title: string;
  // Prose naming what the cut is doing in the user's own numbers.
  caption: string;
  axisValue: string;
  axisOptions: AxisOption[];
  // Optional caveat for the current axis, appended to the explainer tooltip
  // (e.g. the root Skill cut excludes subagent-run skills).
  axisHint?: string;
  onAxisChange: (value: string) => void;
  // Free-text filter over the table's rows, rendered as the standard toolbar
  // search box. Client-side: it narrows the already-loaded rows, never the query.
  searchValue: string;
  onSearchChange: (value: string) => void;
  searchPlaceholder: string;
  // Controls that belong to the table below (e.g. CSV export), rendered beside
  // the axis track.
  actions?: ReactNode;
}): JSX.Element {
  const { segments, overflow } = partitionAxes(axisOptions, axisValue);

  const more = overflow.length > 0 && (
    // Value is pinned to "" so the trigger always reads "More": anything picked
    // here becomes the active axis, which partitionAxes then promotes into the
    // track.
    <Select value="" onValueChange={onAxisChange}>
      <SelectTrigger
        aria-label="More breakdown axes"
        className={cn(
          SEGMENT_BASE,
          SEGMENT_INACTIVE,
          "data-[state=open]:text-foreground w-auto cursor-pointer gap-1 bg-transparent shadow-none focus-visible:ring-0",
        )}
      >
        <SelectValue placeholder="More" />
      </SelectTrigger>
      <SelectContent align="end">
        {overflow.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );

  return (
    <div className="mb-3 flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className="flex items-center gap-1.5 text-sm font-semibold">
          {title}
          {/* No general "what is a breakdown" note — defining it in the abstract
              read as jargon, and the caption below says it against the slice
              actually on screen. The icon is left for axes that carry a real
              caveat, so its presence means something. */}
          {axisHint && (
            <Tooltip>
              <TooltipTrigger
                aria-label={axisHint}
                className="text-muted-foreground inline-flex cursor-help"
              >
                <Info className="size-3.5" />
              </TooltipTrigger>
              <TooltipContent className="max-w-64">{axisHint}</TooltipContent>
            </Tooltip>
          )}
        </h2>
        <p className="text-muted-foreground text-xs">{caption}</p>
      </div>
      {/* The standard list-page control strip: search narrows the rows on the
          left; the preset axis track (re-cut, not narrow) and table actions
          keep their place on the right. */}
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={searchValue}
          onChange={onSearchChange}
          placeholder={searchPlaceholder}
        />
        <Page.Toolbar.Actions>
          {/* A lone axis is no choice at all — at a session leaf (Agent, Model)
              "Sessions" is the only option, and a track you can't move reads as
              a broken toggle. The title already names the cut. */}
          {axisOptions.length > 1 && (
            <SegmentedControl
              value={axisValue}
              onChange={onAxisChange}
              options={segments}
              trailing={more}
            />
          )}
          {actions}
        </Page.Toolbar.Actions>
      </Page.Toolbar>
    </div>
  );
}
