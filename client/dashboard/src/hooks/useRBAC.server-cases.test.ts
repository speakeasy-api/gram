// @vitest-environment node
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { hasScopeInGrants } from "./useRBAC";

// authz-cases.gen.json is written by authzgen (`mise run gen:authz`): sets of
// effective grants, as the server returns them, and the server evaluator's
// answer for each check. The dashboard's own matcher must give the same
// answers, so a change on either side that makes them disagree fails here.
type ServerCase = {
  name: string;
  grants: Parameters<typeof hasScopeInGrants>[0];
  checks: [string, string, string, boolean][];
};

// Read at run time rather than imported, so the type checker does not infer a
// type for the whole file.
const cases = JSON.parse(
  readFileSync(new URL("./authz-cases.gen.json", import.meta.url), "utf8"),
) as ServerCase[];

describe("hasScopeInGrants agrees with the server", () => {
  it("answers every server case the same way", () => {
    const disagreements: string[] = [];
    for (const c of cases) {
      for (const [scope, resourceId, projectId, allowed] of c.checks) {
        const got = hasScopeInGrants(
          c.grants,
          scope as Scope,
          resourceId,
          projectId || undefined,
        );
        if (got !== allowed) {
          disagreements.push(
            `${c.name}: ${scope} on ${resourceId}${projectId ? ` in ${projectId}` : ""} is ${allowed ? "allowed" : "denied"} by the server, ${got ? "allowed" : "denied"} here`,
          );
        }
      }
    }
    expect(disagreements).toEqual([]);
  });

  it("has cases", () => {
    expect(cases.length).toBeGreaterThan(0);
  });
});
