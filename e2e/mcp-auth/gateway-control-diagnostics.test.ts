import assert from "node:assert/strict";
import { test } from "node:test";
import type { AuthGraph } from "./graph.ts";
import { sourceTopology } from "./gateway-control-diagnostics.ts";

test("source topology detects credential source and binding source changes", () => {
  const graph = {
    servers: [
      {
        name: "fixture",
        offers: [
          {
            client: "fixture-client",
            source: "typed",
            bindingSource: "direct",
          },
        ],
      },
    ],
  } as unknown as AuthGraph;
  const before = sourceTopology(graph, ["fixture"]);
  for (const field of ["source", "bindingSource"] as const) {
    const changed = structuredClone(graph);
    changed.servers[0]!.offers[0]![field] = "changed";
    assert.notDeepEqual(sourceTopology(changed, ["fixture"]), before);
  }
});
