import fs from "node:fs";
import path from "node:path";
import { expect, test } from "@playwright/test";
import { ApiClient } from "../lib/api.ts";
import {
  credentialFor,
  lazyWorld,
  PERSONAS,
  type Persona,
} from "../lib/personas.ts";
import { READ_SWEEP_ENDPOINTS } from "./read-sweep.endpoints.ts";

// Golden matrix: the HTTP status every persona gets from every parameter-free
// GET endpoint. It pins today's read authorization across the whole API in one
// file, so any change to who can read what shows up as a reviewable diff of
// read-sweep.baseline.json rather than going unnoticed.
//
// The baseline keeps exact statuses for review, but a run compares outcome
// classes (see `outcome`): whether a call got past authentication and
// authorization is what this suite pins, and some handlers answer 200 or 400
// depending on background work the stack has or has not finished yet.
//
// Re-record with RBAC_E2E_UPDATE_BASELINE=1 after an intended change, and say
// in the pull request which rows moved and why.

export function outcome(status: number): string {
  if (status === 401) return "unauthenticated";
  if (status === 403) return "forbidden";
  if (status === 404) return "not-found";
  if (status >= 500) return "server-error";
  return "reached-handler";
}

const outcomes = (row: Partial<Record<Persona, number>>) =>
  Object.fromEntries(Object.entries(row).map(([p, s]) => [p, outcome(s)]));

type Baseline = Record<string, Partial<Record<Persona, number>>>;

const baselineFile = path.join(import.meta.dirname, "read-sweep.baseline.json");
const updating = process.env["RBAC_E2E_UPDATE_BASELINE"] === "1";
const baseline: Baseline = fs.existsSync(baselineFile)
  ? (JSON.parse(fs.readFileSync(baselineFile, "utf8")) as Baseline)
  : {};
const recorded: Baseline = {};

const world = lazyWorld();

test.describe("read sweep", () => {
  test.afterAll(() => {
    if (!updating) return;
    // Only a complete recording replaces the baseline, so a run that failed
    // part-way cannot leave old and new rows mixed, and endpoints dropped from
    // the list drop out of the file.
    const missing = READ_SWEEP_ENDPOINTS.filter((e) => !(e in recorded));
    if (missing.length > 0) {
      throw new Error(
        `baseline not written: ${missing.length} endpoints were not recorded`,
      );
    }
    const sorted = Object.fromEntries(
      Object.entries(recorded).sort(([a], [b]) => a.localeCompare(b)),
    );
    fs.writeFileSync(baselineFile, `${JSON.stringify(sorted, null, 2)}\n`);
  });

  for (const endpoint of READ_SWEEP_ENDPOINTS) {
    test(endpoint, async ({ request }) => {
      const observed: Partial<Record<Persona, number>> = {};
      for (const persona of PERSONAS) {
        const api = new ApiClient(request, credentialFor(world, persona));
        const res = await api.call("GET", `/rpc/${endpoint}`);
        observed[persona] = res.status();
      }
      if (updating) {
        recorded[endpoint] = observed;
        return;
      }
      const expected = baseline[endpoint];
      expect(
        expected,
        `${endpoint} has no baseline row; re-record with RBAC_E2E_UPDATE_BASELINE=1`,
      ).toBeDefined();
      expect(
        outcomes(observed),
        `exact statuses: ${JSON.stringify(observed)}`,
      ).toEqual(outcomes(expected!));
    });
  }
});
