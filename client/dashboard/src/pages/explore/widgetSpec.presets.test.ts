import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { specFromWidget, widgetFromSpec } from "./widgetSpec";

// The server validates every preset against the catalog (TestPresets in
// server/internal/widgets). The builder is pickier about shape than the
// catalog is, and a preset it cannot read draws as broken and cannot be
// opened in Explore, so each must also restore into the builder and save
// back exactly as declared.
const PRESETS = resolve(
  import.meta.dirname,
  "../../../../../server/internal/widgets/presets.json",
);

interface Preset {
  page: string;
  rows: {
    widgets: {
      key: string;
      dataset: string;
      query: Record<string, unknown>;
      visualization: Record<string, unknown>;
    }[];
  }[];
}

const presets = (
  JSON.parse(readFileSync(PRESETS, "utf8")) as { pages: Preset[] }
).pages;
const widgets = presets.flatMap((page) =>
  page.rows.flatMap((row) =>
    row.widgets.map((widget) => ({ ...widget, page: page.page })),
  ),
);

describe("the built-in presets", () => {
  it("are read from the file the server embeds", () => {
    expect(Array.isArray(presets)).toBe(true);
  });

  it.each(widgets.map((widget) => [`${widget.page}/${widget.key}`, widget]))(
    "%s opens in the builder and saves back as declared",
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
