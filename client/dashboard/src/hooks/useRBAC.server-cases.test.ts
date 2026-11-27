// @vitest-environment node
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import * as authz from "@/lib/authz.gen";
import { canInGrants, hasScopeInGrants } from "./useRBAC";

// authz-cases.gen.json is written by authzgen (`mise run gen:authz`): sets of
// effective grants, as the server returns them, and the server evaluator's
// answer for each check. The dashboard's own matcher must give the same
// answers, so a change on either side that makes them disagree fails here.
type ServerCase = {
  name: string;
  grants: Parameters<typeof hasScopeInGrants>[0];
  checks: [string, string, string, boolean][];
  calls: [string, string[], string, boolean, boolean][];
};

type Constructor = (
  ...args: string[]
) => Record<string, (...args: string[]) => authz.Check>;

// Builds a call's check through the generated constructor of that name, so a
// constructor that asks a different question than its Go twin fails here.
function buildCheck(name: string, args: string[], method: string): authz.Check {
  const constructor = (authz as unknown as Record<string, Constructor>)[name];
  if (!constructor) throw new Error(`no generated constructor ${name}`);
  const methods = constructor(...args);
  const build = methods[method];
  if (!build) throw new Error(`${name} has no method ${method}`);
  // A method's own arguments follow the constructor's in the case.
  return build(...args.slice(constructor.length));
}

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

  it("answers every constructor call the same way", () => {
    const disagreements: string[] = [];
    for (const c of cases) {
      for (const [name, args, method, probe, allowed] of c.calls) {
        const check = buildCheck(name, args, method);
        const got = canInGrants(c.grants, probe ? check.probe() : check);
        if (got !== allowed) {
          disagreements.push(
            `${c.name}: ${name}(${args.join(", ")}).${method}()${probe ? ".probe()" : ""} is ${allowed ? "allowed" : "denied"} by the server, ${got ? "allowed" : "denied"} here`,
          );
        }
      }
    }
    expect(disagreements).toEqual([]);
  });

  it("has cases", () => {
    expect(cases.length).toBeGreaterThan(0);
    // Every generated constructor is answered by the server somewhere.
    const constructorNames = Object.entries(authz)
      .filter(
        ([name, value]) => name !== "Check" && typeof value === "function",
      )
      .map(([name]) => name);
    expect(
      new Set(cases.flatMap((c) => c.calls.map(([name]) => name))),
    ).toEqual(new Set(constructorNames));
  });
});
