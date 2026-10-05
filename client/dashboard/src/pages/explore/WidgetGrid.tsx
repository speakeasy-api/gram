import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import type { PresetWidget } from "@gram/client/models/components/presetwidget.js";
import type { WidgetPreset } from "@gram/client/models/components/widgetpreset.js";
import { useWidgetPreset } from "@gram/client/react-query/widgetPreset.js";
import { useId, useState, type JSX, type ReactNode } from "react";
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

const COMPARE_KEY = "gram-widget-compare-legacy";

/**
 * A page's preset, fetched and laid out. `legacy` maps a widget's key to
 * the figure the page's old endpoint gives for the same window, so a page
 * can be cut over card by card with the two side by side.
 */
export function PresetWidgets({
  page,
  context,
  legacy,
}: {
  /** The preset to lay out, by page name. */
  page: string;
  context?: PageContext;
  legacy?: Record<string, ReactNode>;
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
  return <WidgetGrid preset={preset.data} context={context} legacy={legacy} />;
}

/** A preset's rows of widgets, each answering within the page's context. */
export function WidgetGrid({
  preset,
  context,
  legacy,
}: {
  preset: WidgetPreset;
  context?: PageContext | undefined;
  legacy?: Record<string, ReactNode> | undefined;
}): JSX.Element {
  const compare = useCompareLegacy();
  const comparable = compare.available && Object.keys(legacy ?? {}).length > 0;
  return (
    <div className="flex flex-col gap-4">
      {comparable ? (
        <CompareToggle on={compare.on} onChange={compare.set} />
      ) : null}
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
              footer={
                comparable &&
                compare.on &&
                legacy?.[widget.key] !== undefined ? (
                  <LegacyFigure value={legacy[widget.key]} />
                ) : null
              }
            />
          ))}
        </div>
      ))}
    </div>
  );
}

/**
 * Whether the legacy figures show beside the widgets. Staff and local
 * development only: it exists to check a page's cutover, not for customers.
 * The choice is remembered per browser.
 */
function useCompareLegacy(): {
  available: boolean;
  on: boolean;
  set: (on: boolean) => void;
} {
  const staff = useIsSpeakeasyStaff();
  const [on, setOn] = useState(() => {
    try {
      return localStorage.getItem(COMPARE_KEY) === "1";
    } catch {
      return false;
    }
  });
  return {
    available: staff || import.meta.env.DEV,
    on,
    set: (next) => {
      setOn(next);
      try {
        localStorage.setItem(COMPARE_KEY, next ? "1" : "0");
      } catch {
        // Unavailable storage only costs remembering the choice.
      }
    },
  };
}

function CompareToggle({
  on,
  onChange,
}: {
  on: boolean;
  onChange: (on: boolean) => void;
}): JSX.Element {
  const label = useId();
  return (
    <div className="flex items-center justify-end gap-2">
      <span id={label} className="text-muted-foreground text-xs">
        Compare with legacy
      </span>
      <Switch checked={on} onCheckedChange={onChange} aria-labelledby={label} />
    </div>
  );
}

function LegacyFigure({ value }: { value: ReactNode }): JSX.Element {
  return (
    <div className="border-border text-muted-foreground flex items-baseline gap-2 border-t pt-2 text-xs">
      <span className="text-eyebrow">Legacy</span>
      <span className="text-foreground tabular-nums">{value}</span>
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
