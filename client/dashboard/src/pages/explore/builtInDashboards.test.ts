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
  // Each card names its dataset, then its query and visualization as JSON
  // literals, in that order.
  const card =
    /Dataset:\s*"([a-z_]+)",\s*Query:\s*json\.RawMessage\(`(\{[^`]*\})`\),\s*Visualization:\s*json\.RawMessage\(`(\{[^`]*\})`\)/g;
  return Array.from(
    source.matchAll(card),
    ([, dataset, query, visualization]) => ({
      dataset: dataset!,
      query: JSON.parse(query!) as Record<string, unknown>,
      visualization: JSON.parse(visualization!) as Record<string, unknown>,
    }),
  );
}

describe("the Speakeasy-built dashboards' cards", () => {
  const cards = builtInCards();

  it("are all found", () => {
    expect(cards).toHaveLength(8);
  });

  it.each(cards.map((card, i) => [i + 1, card] as const))(
    "card %i opens in the builder and saves back as stored",
    (_, card) => {
      const spec = specFromWidget(card.dataset, card.query, card.visualization);
      expect(spec).not.toBeNull();
      expect(widgetFromSpec(spec!)).toEqual({
        query: card.query,
        visualization: card.visualization,
      });
    },
  );
});
