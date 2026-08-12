import { Page } from "@/components/page-layout";
import {
  SEGMENT_BASE,
  SEGMENT_INACTIVE,
  SegmentedControl,
} from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { cn } from "@/lib/utils";

// The breakdown section's control strip: the axes to re-cut the chart and
// table by (the track), and the free-text search that narrows the table's
// rows. It sits directly above the chart/table — not in the page-scope bar
// under the headline stats — so the controls that reshape this section read
// as belonging to it.
//
// The axis track replaced a bare "Breakdown by <select>": users didn't find
// the dropdown, and the word "breakdown" reads as jargon until you've watched
// it re-cut the same spend. So the axes are promoted to visible segments, and
// the section title above states the current cut ("Cost by Model") rather than
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
}: {
  axisValue: string;
  axisOptions: AxisOption[];
  onAxisChange: (value: string) => void;
  // Free-text filter over the table's rows, rendered as the standard toolbar
  // search box. Client-side: it narrows the already-loaded rows, never the query.
  searchValue: string;
  onSearchChange: (value: string) => void;
  searchPlaceholder: string;
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
          "data-[state=open]:text-foreground w-auto cursor-pointer gap-1 border-0 bg-transparent shadow-none focus-visible:ring-0",
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
    // Axis track on the left, row search on the right — one Page.Toolbar row
    // that spans the breakdown section. A lone axis is no choice at all (at a
    // session leaf "Sessions" is the only option), and a track you can't move
    // reads as a broken toggle; the section title already names the cut, so
    // the track is omitted in that case and search keeps the full row.
    <Page.Toolbar>
      {axisOptions.length > 1 && (
        <Page.Toolbar.Leading>
          <SegmentedControl
            value={axisValue}
            onChange={onAxisChange}
            options={segments}
            trailing={more}
          />
        </Page.Toolbar.Leading>
      )}
      {/* Wrapped in Actions so the search anchors right — the left column
          belongs to the axis track. */}
      <Page.Toolbar.Actions>
        <Page.Toolbar.Search
          value={searchValue}
          onChange={onSearchChange}
          placeholder={searchPlaceholder}
        />
      </Page.Toolbar.Actions>
    </Page.Toolbar>
  );
}
