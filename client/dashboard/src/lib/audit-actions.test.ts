import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  AUDIT_ACTIONS,
  isAuditAction,
  staticActionPhrase,
} from "./audit-actions";

// Walk up from the working directory to the repo root, so the guard resolves
// whether vitest is invoked from client/dashboard or from the repo root.
function findAuditPkg(): string {
  let dir = process.cwd();
  for (;;) {
    const candidate = join(dir, "server/internal/audit");
    if (existsSync(candidate)) return candidate;
    const parent = dirname(dir);
    if (parent === dir) {
      throw new Error(
        `could not locate server/internal/audit above ${process.cwd()}`,
      );
    }
    dir = parent;
  }
}

const AUDIT_PKG = findAuditPkg();

/** Every `Foo Action = "resource:verb"` constant declared in the Go package. */
function goAuditActions(): string[] {
  const actions = new Set<string>();
  for (const file of readdirSync(AUDIT_PKG)) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const source = readFileSync(join(AUDIT_PKG, file), "utf8");
    for (const match of source.matchAll(/\bAction\s*=\s*"([^"]+)"/g)) {
      const action = match[1];
      if (action) actions.add(action);
    }
  }
  return [...actions].sort();
}

describe("AUDIT_ACTIONS", () => {
  // Guards the exhaustive switch in staticActionPhrase: it only proves every
  // action has a phrase if this list matches what the server can emit.
  it("matches the Action constants declared in server/internal/audit", () => {
    expect([...AUDIT_ACTIONS].sort()).toEqual(goAuditActions());
  });

  it("gives every action a past-tense phrase", () => {
    for (const action of AUDIT_ACTIONS) {
      const phrase = staticActionPhrase(action);
      expect(phrase, action).not.toContain(":");
      expect(phrase, action).not.toBe("");
    }
  });

  it("describes enterprise conversion against its organization subject", () => {
    expect(staticActionPhrase("organization:enterprise_trial_converted")).toBe(
      "converted enterprise trial for",
    );
  });

  it("rejects actions it doesn't know", () => {
    expect(isAuditAction("risk_policy:delete")).toBe(true);
    expect(isAuditAction("not_a_resource:not_a_verb")).toBe(false);
  });
});
