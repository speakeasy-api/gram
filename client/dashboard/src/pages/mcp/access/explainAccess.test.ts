import type { ExplainedAccessLevel } from "@gram/client/models/components/explainedaccesslevel.js";
import type { ExplainedAccessRule } from "@gram/client/models/components/explainedaccessrule.js";
import type { ExplainResourceAccessResult } from "@gram/client/models/components/explainresourceaccessresult.js";
import { describe, expect, it } from "vitest";
import {
  accessDecision,
  directorySourceLabel,
  effectNote,
  levelStatus,
  ruleAccessLabel,
  whySentence,
} from "./explainAccess";

function rule(
  overrides: Partial<ExplainedAccessRule> & {
    displayName: string;
    effect: ExplainedAccessRule["effect"];
  },
): ExplainedAccessRule {
  return {
    principalUrn: `role:organization:${overrides.displayName}`,
    kind: "role",
    level: "use",
    appliesTo: "resource",
    viaDirectoryMapping: false,
    ...overrides,
  } as ExplainedAccessRule;
}

function level(
  overrides: Partial<ExplainedAccessLevel> & {
    level: ExplainedAccessLevel["level"];
  },
): ExplainedAccessLevel {
  return { allowed: false, rules: [], ...overrides } as ExplainedAccessLevel;
}

function result(
  levels: ExplainedAccessLevel[],
  visibility: ExplainResourceAccessResult["visibility"] = "private",
): ExplainResourceAccessResult {
  return { visibility, levels } as ExplainResourceAccessResult;
}

const contractorsBlock = rule({
  displayName: "Contractors",
  level: "blocked",
  effect: "blocks",
});
const engineerBlocked = rule({
  displayName: "Engineer",
  appliesTo: "all_resources",
  effect: "blocked",
});

describe("accessDecision", () => {
  it("reports a block from another role", () => {
    const blocked = level({
      level: "use",
      toolAccess: "none",
      rules: [contractorsBlock, engineerBlocked],
    });
    expect(accessDecision(result([blocked]), "Lucas", "GitHub")).toEqual({
      tone: "blocked",
      label: "Blocked",
      sentence: "Lucas can't connect to GitHub.",
    });
  });

  it("reports no rule as blocked by default", () => {
    const none = level({ level: "use", toolAccess: "none" });
    expect(accessDecision(result([none]), "Daniel", "GitHub").label).toBe(
      "Blocked by default",
    );
  });

  it("reports narrowed access as partial", () => {
    const some = level({ level: "use", allowed: true, toolAccess: "some" });
    expect(accessDecision(result([some]), "Amara", "GitHub").tone).toBe(
      "partial",
    );
  });

  it("says a public server is not checked", () => {
    const none = level({ level: "use", toolAccess: "none" });
    expect(
      accessDecision(result([none], "public"), "Daniel", "GitHub").label,
    ).toBe("Public");
  });
});

describe("whySentence", () => {
  it("names the block and the grant it withdrew", () => {
    const blocked = level({
      level: "use",
      rules: [contractorsBlock, engineerBlocked],
    });
    expect(whySentence(blocked, "Lucas", "GitHub", "private")).toBe(
      "Blocks win over grants. Contractors blocks Connect, so Engineer's grant does not apply.",
    );
  });

  it("explains a direct grant outranking a role block", () => {
    const overridden = level({
      level: "use",
      allowed: true,
      rules: [
        rule({
          displayName: "Priya",
          kind: "user",
          effect: "overrides",
        }),
        { ...contractorsBlock, effect: "overridden" },
      ],
    });
    expect(whySentence(overridden, "Priya", "GitHub", "private")).toBe(
      "A grant made directly to Priya on GitHub overrides blocks from roles and everyone, so the Contractors block does not apply.",
    );
  });

  it("explains why a wildcard direct grant does not outrank", () => {
    const blocked = level({
      level: "use",
      rules: [
        contractorsBlock,
        rule({
          displayName: "Mateo",
          kind: "user",
          appliesTo: "all_resources",
          effect: "blocked",
          reason: "wildcard_direct_grant",
        }),
      ],
    });
    expect(whySentence(blocked, "Mateo", "GitHub", "private")).toContain(
      "Only a grant naming GitHub can.",
    );
  });

  it("says nothing grants access when no rule matches", () => {
    expect(
      whySentence(level({ level: "view" }), "Daniel", "GitHub", "private"),
    ).toBe(
      "Nothing grants Daniel View on GitHub. Access is off until a rule grants it.",
    );
  });

  it("names a rule limiting tools", () => {
    const limited = level({
      level: "use",
      allowed: true,
      toolAccess: "some",
      rules: [
        rule({ displayName: "Analyst", effect: "allows" }),
        rule({
          displayName: "Analyst",
          level: "blocked",
          dispositions: ["destructive"],
          effect: "limits",
        }),
      ],
    });
    expect(whySentence(limited, "Amara", "GitHub", "private")).toBe(
      "Analyst grants Connect on GitHub. Analyst limits which tools Amara can call.",
    );
  });
});

describe("rule labels", () => {
  it("describes connect narrowing", () => {
    expect(
      ruleAccessLabel(
        rule({
          displayName: "Analyst",
          level: "blocked",
          dispositions: ["destructive"],
          effect: "limits",
        }),
      ),
    ).toBe("Connect · Destructive tools");
  });

  it("explains a blocked wildcard direct grant", () => {
    expect(
      effectNote(
        rule({
          displayName: "Mateo",
          kind: "user",
          effect: "blocked",
          reason: "wildcard_direct_grant",
        }),
        level({ level: "use", rules: [contractorsBlock] }),
        "Mateo",
        "GitHub",
      ),
    ).toBe(
      "Covers every server, so it cannot override a block. Only a grant naming GitHub can.",
    );
  });

  it("names the block that kept a grant from counting", () => {
    expect(
      effectNote(
        engineerBlocked,
        level({ level: "use", rules: [contractorsBlock, engineerBlocked] }),
        "Lucas",
        "GitHub",
      ),
    ).toBe("Blocked by Contractors.");
  });

  it("names the directory group a role came from", () => {
    expect(
      directorySourceLabel({
        ...contractorsBlock,
        viaDirectoryMapping: true,
        directorySources: [
          { sourceKind: "group", directoryGroupName: "okta/contractors" },
        ],
      }),
    ).toBe("Mapped from directory group okta/contractors");
  });

  it("keeps the mapping private without its sources", () => {
    expect(
      directorySourceLabel({ ...contractorsBlock, viaDirectoryMapping: true }),
    ).toBe("Mapped from the directory");
  });

  it("shows a blocked level as blocked", () => {
    expect(
      levelStatus(
        level({ level: "use", rules: [contractorsBlock] }),
        "private",
      ),
    ).toEqual({ tone: "blocked", label: "Blocked" });
  });
});

describe("visibility", () => {
  it("lets a public server answer Connect before the rules", () => {
    const blocked = level({ level: "use", rules: [contractorsBlock] });
    expect(levelStatus(blocked, "public")).toEqual({
      tone: "allowed",
      label: "Public",
    });
    expect(whySentence(blocked, "Lucas", "GitHub", "public")).toBe(
      "GitHub is public, so anyone can connect without a rule. If it were private: Contractors blocks Connect on GitHub.",
    );
  });

  it("leaves View to the rules on a public server", () => {
    expect(levelStatus(level({ level: "view" }), "public").label).toBe(
      "No access",
    );
  });

  it("shows a disabled server as unreachable", () => {
    const allowed = level({ level: "use", allowed: true, toolAccess: "all" });
    expect(levelStatus(allowed, "disabled")).toEqual({
      tone: "blocked",
      label: "Disabled",
    });
  });
});

describe("narrowed direct grants", () => {
  it("says the block still takes the other tools away", () => {
    const partial = level({
      level: "use",
      allowed: true,
      toolAccess: "some",
      rules: [
        rule({
          displayName: "Mateo",
          kind: "user",
          tools: ["get_issue"],
          effect: "overrides",
        }),
        { ...contractorsBlock, effect: "limits" },
      ],
    });
    expect(whySentence(partial, "Mateo", "GitHub", "private")).toBe(
      "A grant made directly to Mateo on GitHub overrides blocks from roles and everyone, but only for the tools it names. Contractors still takes the other tools away.",
    );
  });
});
