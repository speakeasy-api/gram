// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { toolDisposition } from "./mcpAccessModel";
import type { ServerTool } from "./serverMerge";

// disposition-cases.gen.json is written by authzcases (`mise run gen:authz`):
// every combination of annotation hints, each absent, false or true, and the
// disposition the server gives a tool that carries them. The role editor must
// classify tools the same way, or it shows a tool limit as granting tools the
// server does not let through, or the other way round.
type DispositionCase = {
  hints: NonNullable<ServerTool["annotations"]>;
  disposition: string;
};

// Read at run time rather than imported, so the type checker does not infer a
// type for the whole file.
const cases = JSON.parse(
  readFileSync(
    new URL("./disposition-cases.gen.json", import.meta.url),
    "utf8",
  ),
) as DispositionCase[];

describe("toolDisposition agrees with the server", () => {
  it("classifies every combination of hints the same way", () => {
    const disagreements: string[] = [];
    for (const c of cases) {
      const tool: ServerTool = {
        id: "1",
        name: "tool",
        type: "http",
        annotations: c.hints,
      };
      const got = toolDisposition(tool);
      if (got !== c.disposition) {
        disagreements.push(
          `${JSON.stringify(c.hints)}: the server says ${c.disposition}, here ${got}`,
        );
      }
    }
    expect(disagreements).toEqual([]);
  });

  it("has every combination", () => {
    expect(cases).toHaveLength(81);
  });
});
