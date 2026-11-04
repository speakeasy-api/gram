import { expect, test, type APIResponse } from "@playwright/test";
import { ApiClient } from "../lib/api.ts";
import { credentialFor, lazyWorld, type Persona } from "../lib/personas.ts";
import { CASES, type CaseContext } from "./cases.ts";

// Runs every row of cases.ts: for each listed persona, prepare fixtures as
// admin, make the call as the persona, assert the status, clean up.

const world = lazyWorld();
let counter = 0;

for (const c of CASES) {
  test.describe(`${c.slice} › ${c.name}`, () => {
    for (const [persona, status] of Object.entries(c.expect) as [
      Persona,
      number,
    ][]) {
      test(`${persona} → ${status}`, async ({ request }) => {
        const ctx: CaseContext = {
          world,
          admin: new ApiClient(request, credentialFor(world, "admin")),
          unique: (prefix) =>
            `${prefix}-${Date.now().toString(36)}-${(counter++).toString(36)}`,
        };
        const project =
          c.project === undefined || c.project === "default"
            ? world.projectSlug
            : c.project === "sandbox"
              ? world.sandboxProjectSlug
              : null;
        let fx: Record<string, string> = {};
        let res: APIResponse | undefined;
        try {
          fx = c.prepare ? await c.prepare(ctx) : {};
          const { query, body } = c.request ? c.request(ctx, fx) : {};
          const api = new ApiClient(request, credentialFor(world, persona));
          res = await api.call(c.method, c.path, {
            project,
            ...(query ? { query } : {}),
            ...(body === undefined ? {} : { body }),
            ...(c.bearer
              ? {
                  bearer: true,
                  headers: { Accept: "application/json, text/event-stream" },
                }
              : {}),
          });
          expect(
            res.status(),
            `${c.method} ${c.path} as ${persona}: ${(await res.text()).slice(0, 300)}`,
          ).toBe(status);
          if (c.check) {
            const text = await res.text();
            c.check(persona, res.status(), text ? JSON.parse(text) : undefined);
          }
        } finally {
          if (c.cleanup) await c.cleanup(ctx, fx, res);
        }
      });
    }
  });
}
