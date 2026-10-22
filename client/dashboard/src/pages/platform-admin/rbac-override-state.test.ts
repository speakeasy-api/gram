import { Scope } from "@gram/client/models/components/rolegrant.js";
import { describe, expect, it } from "vitest";
import { GROUP_ORDER, SCOPE_DEFS } from "./rbac-override-state";

// The override replaces the caller's grants outright, so a grantable scope
// missing here is silently denied while the toolbar says "all scopes" —
// agent pages rendered empty that way when the agent scopes were added.
// Deny-effect scopes are left out, and so are the scopes meant for agents:
// a person reaches the device-sync and hook-ingest routes through org:admin.
const NOT_OVERRIDABLE = new Set<string>([
  Scope.OrgDeviceAgentSync,
  Scope.OrgHooksIngest,
]);

describe("RBAC override scope list", () => {
  it("covers every grantable scope the server defines", () => {
    const listed = new Set(SCOPE_DEFS.map((def) => def.scope));
    const missing = Object.values(Scope).filter(
      (scope) =>
        !scope.includes(":blocked_") &&
        !NOT_OVERRIDABLE.has(scope) &&
        !listed.has(scope),
    );
    expect(missing).toEqual([]);
  });

  it("lists only scopes the server defines, each once", () => {
    const known = new Set<string>(Object.values(Scope));
    const scopes = SCOPE_DEFS.map((def) => def.scope);
    expect(scopes.filter((scope) => !known.has(scope))).toEqual([]);
    expect(new Set(scopes).size).toBe(scopes.length);
  });

  it("renders every scope under a group", () => {
    const groups = new Set(GROUP_ORDER.map((group) => group.key));
    expect(
      SCOPE_DEFS.filter((def) => !groups.has(def.resourceType)).map(
        (def) => def.scope,
      ),
    ).toEqual([]);
  });
});
