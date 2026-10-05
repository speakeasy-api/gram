import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import type { PresetWidget } from "@gram/client/models/components/presetwidget.js";
import type { WidgetPreset } from "@gram/client/models/components/widgetpreset.js";
import { useWidgetPreset } from "@gram/client/react-query/widgetPreset.js";
import type { JSX } from "react";
import type { PageContext } from "./pageContext";
import { WidgetView } from "./WidgetView";

// A product page is a preset: rows of widgets on a 12-column grid, checked
// into the repository (server/internal/widgets/presets.json) and validated
// against the catalog in CI. Each widget declares its width; its height
// follows its chart type. Sizes are the preset's: resizing belongs to
// user-editable dashboards.

// Below lg, quarters pair up as halves and everything else takes the full
// width, as the insights grids do today. Whole class names, so Tailwind
// sees them.
const SPAN_CLASSES: Record<PresetWidget["span"], string> = {
  3: "col-span-6 lg:col-span-3",
  4: "col-span-12 lg:col-span-4",
  6: "col-span-12 lg:col-span-6",
  12: "col-span-12",
};

/** A page's preset, fetched and laid out. */
export function PresetWidgets({
  page,
  context,
}: {
  /** The preset to lay out, by page name. */
  page: string;
  context?: PageContext;
}): JSX.Element {
  const preset = useWidgetPreset({ page });
  if (preset.isError) {
    return (
      <div
        role="alert"
        className="border-border bg-card flex items-center justify-between gap-4 border p-4 text-sm"
      >
        <span className="text-muted-foreground">
          This page's widgets did not load.
        </span>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void preset.refetch()}
        >
          Try again
        </Button>
      </div>
    );
  }
  if (preset.data === undefined) return <GridSkeleton />;
  return <WidgetGrid preset={preset.data} context={context} />;
}

/** A preset's rows of widgets, each answering within the page's context. */
export function WidgetGrid({
  preset,
  context,
}: {
  preset: WidgetPreset;
  context?: PageContext | undefined;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      {preset.rows.map((row, index) => (
        <div
          key={row.widgets.map((widget) => widget.key).join(",") || index}
          className="grid grid-cols-12 gap-4"
        >
          {row.widgets.map((widget) => (
            <WidgetView
              key={widget.key}
              widget={widget}
              page={context}
              className={SPAN_CLASSES[widget.span]}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

function GridSkeleton(): JSX.Element {
  return (
    <div
      className="grid grid-cols-12 gap-4"
      aria-busy="true"
      aria-label="Loading widgets"
    >
      {SKELETON_SPANS.map((span, index) => (
        <Skeleton key={index} className={`${SPAN_CLASSES[span]} h-32`} />
      ))}
    </div>
  );
}

const SKELETON_SPANS: PresetWidget["span"][] = [3, 3, 3, 3, 12];
