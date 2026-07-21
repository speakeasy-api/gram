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
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

// The costs page's single control strip: the search box, the axes to re-cut
// the page by (the track), the table actions (CSV export), and the page-scope
// controls (dataset + date range). It sits at the top of the page because the
// breakdown axis re-cuts every visualization below it — the chart and the
// table — and the dataset/range scope every number on the page.
//
// The axis track replaced a bare "Breakdown by <select>": users didn't find
// the dropdown, and the word "breakdown" reads as jargon until you've watched
// it re-cut the same spend. So the axes are promoted to visible segments, and
// the section title below states the current cut ("Cost by Model") rather than
// naming the mechanism — pairing a lit segment with a title that echoes it
// teaches the idea on the first click.

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
  axisValue,
  axisOptions,
  onAxisChange,
  searchValue,
  onSearchChange,
  searchPlaceholder,
  actions,
  scopeControls,
}: {
  axisValue: string;
  axisOptions: AxisOption[];
  onAxisChange: (value: string) => void;
  // Free-text filter over the table's rows, rendered as the standard toolbar
  // search box. Client-side: it narrows the already-loaded rows, never the query.
  searchValue: string;
  onSearchChange: (value: string) => void;
  searchPlaceholder: string;
  // Controls that belong to the table below (e.g. CSV export), anchored to
  // the right of the bar's top row.
  actions?: ReactNode;
  // Page-scope controls (dataset selector + date-range picker), leading the
  // bar's top row.
  scopeControls?: ReactNode;
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
    // A deliberate two-row control bar on the shared Page.Toolbar shell. The
    // data options read as one left-stacked block: dataset + range lead the
    // top row with the breakdown axis track directly beneath them; table
    // actions (export/reset) anchor top-right and the row search anchors
    // bottom-right. Every row spans the full bar at every width, so nothing
    // clips and nothing rags.
    <Page.Toolbar>
      <Page.Toolbar.Row>
        <Page.Toolbar.Leading>{scopeControls}</Page.Toolbar.Leading>
        <Page.Toolbar.Actions>{actions}</Page.Toolbar.Actions>
      </Page.Toolbar.Row>
      <Page.Toolbar.Row>
        <Page.Toolbar.Leading>
          {/* A lone axis is no choice at all — at a session leaf (Agent,
              Model) "Sessions" is the only option, and a track you can't move
              reads as a broken toggle. The section title already names the
              cut. */}
          {axisOptions.length > 1 && (
            <SegmentedControl
              value={axisValue}
              onChange={onAxisChange}
              options={segments}
              trailing={more}
            />
          )}
        </Page.Toolbar.Leading>
        {/* Wrapped in Actions so the search anchors right — the left column
            below the dataset/range belongs to the axis track. */}
        <Page.Toolbar.Actions>
          <Page.Toolbar.Search
            value={searchValue}
            onChange={onSearchChange}
            placeholder={searchPlaceholder}
          />
        </Page.Toolbar.Actions>
      </Page.Toolbar.Row>
    </Page.Toolbar>
  );
}
