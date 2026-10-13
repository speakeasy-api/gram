import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { specFromWidget, widgetFromSpec } from "./widgetSpec";

// The demo seed writes its widgets by hand, so nothing else notices when the
// saved format moves away from them: each must open in the builder and save
// back exactly as it is stored, or it would open marked unsaved.
const SEED = resolve(
  import.meta.dirname,
  "../../../../../server/internal/demoseed/postgres.sql",
);

interface SeededWidget {
  dataset: string;
  query: Record<string, unknown>;
  visualization: Record<string, unknown>;
}

function seededWidgets(): SeededWidget[] {
  const sql = readFileSync(SEED, "utf8");
  const start = sql.search(/INSERT\s+INTO\s+widgets\b/i);
  if (start < 0) throw new Error("the demo seed has no INSERT INTO widgets");
  const end = sql.indexOf(";", start);
  const insert = sql.slice(start, end);
  // Each row names its dataset, then its query and visualization as JSON.
  const row = /'([a-z_]+)'\s*,\s*'(\{[^']*\})'\s*,\s*'(\{[^']*\})'/g;
  return Array.from(insert.matchAll(row), ([, dataset, query, chart]) => ({
    dataset: dataset!,
    query: JSON.parse(query!) as Record<string, unknown>,
    visualization: JSON.parse(chart!) as Record<string, unknown>,
  }));
}

describe("the demo seed's widgets", () => {
  const widgets = seededWidgets();

  it("are all found", () => {
    expect(widgets).toHaveLength(6);
  });

  it.each(widgets.map((widget, i) => [i + 1, widget] as const))(
    "widget %i opens in the builder and saves back as stored",
    (_, widget) => {
      const spec = specFromWidget(
        widget.dataset,
        widget.query,
        widget.visualization,
      );
      expect(spec).not.toBeNull();
      expect(widgetFromSpec(spec!)).toEqual({
        query: widget.query,
        visualization: widget.visualization,
      });
    },
  );
});
