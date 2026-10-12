import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { specFromWidget, widgetFromSpec } from "./widgetSpec";

// The Speakeasy-built dashboards' cards are written by hand in Go, so
// nothing else notices when the saved widget format moves away from them:
// each must open in the builder and save back exactly as it is stored, or
// Open in Explore on the card would open a different question, and the
// widget Duplicate makes of it would open marked unsaved.
const REGISTRY = resolve(
  import.meta.dirname,
  "../../../../../server/internal/dashboards/builtin.go",
);

interface BuiltInCard {
  dataset: string;
  query: Record<string, unknown>;
  visualization: Record<string, unknown>;
}

function builtInCards(): BuiltInCard[] {
  const source = readFileSync(REGISTRY, "utf8");
  // A dashboard's cards are the slice literal opened by []builtInCard{ and
  // closed at its own indentation. Inside it every card sets Dataset once,
  // so splitting there yields one chunk per card. Each chunk is read field
  // by field, and a chunk missing any field fails the test rather than
  // dropping the card, so a card written in a shape this parser does not
  // know cannot slip past it.
  const chunks = source
    .split("[]builtInCard{")
    .slice(1)
    .flatMap((rest, index) => {
      const end = rest.indexOf("\n\t},");
      if (end < 0) {
        throw new Error(`card list ${index + 1} in builtin.go has no end`);
      }
      return rest
        .slice(0, end)
        .split(/\bDataset:/)
        .slice(1);
    });
  return chunks.map((chunk, index) => {
    const dataset = /^\s*"([a-z_]+)",/.exec(chunk)?.[1];
    const query = /\bQuery:\s*json\.RawMessage\(`([^`]*)`\)/.exec(chunk)?.[1];
    const visualization =
      /\bVisualization:\s*json\.RawMessage\(`([^`]*)`\)/.exec(chunk)?.[1];
    if (!dataset || !query || !visualization) {
      throw new Error(
        `card ${index + 1} in builtin.go is not in a shape this test reads: ${chunk.slice(0, 200)}`,
      );
    }
    return {
      dataset,
      query: JSON.parse(query) as Record<string, unknown>,
      visualization: JSON.parse(visualization) as Record<string, unknown>,
    };
  });
}

describe("the Speakeasy-built dashboards' cards", () => {
  const cards = builtInCards();

  it("are found", () => {
    expect(cards.length).toBeGreaterThan(0);
  });

  it.each(cards.map((card, i) => [i + 1, card] as const))(
    "card %i opens in the builder and saves back as stored",
    (_, card) => {
      const spec = specFromWidget(card.dataset, card.query, card.visualization);
      expect(spec).not.toBeNull();
      expect(spec!.dataset).toBe(card.dataset);
      expect(widgetFromSpec(spec!)).toEqual({
        query: card.query,
        visualization: card.visualization,
      });
    },
  );
});
