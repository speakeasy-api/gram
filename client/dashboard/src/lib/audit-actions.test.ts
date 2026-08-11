import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  AUDIT_ACTIONS,
  isAuditAction,
  staticActionPhrase,
} from "./audit-actions";

// vitest runs from client/dashboard.
const AUDIT_PKG = join(process.cwd(), "../../server/internal/audit");

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

  it("rejects actions it doesn't know", () => {
    expect(isAuditAction("risk_policy:delete")).toBe(true);
    expect(isAuditAction("not_a_resource:not_a_verb")).toBe(false);
  });
});
