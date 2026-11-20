// @vitest-environment node
import { describe, expect, it } from "vitest";

import type { Snapshot } from "@/pages/coverage/model";

import type { CoverageCell } from "./coverageApi";
import { cellKey, indexCells, methodFootprints } from "./coverageModel";

function cell(
  capability: CoverageCell["capability"],
  surface: CoverageCell["surface"],
  status: CoverageCell["status"],
): CoverageCell {
  return {
    capability,
    surface,
    status,
    value: 0,
    unit: "",
    detail: "",
    last_seen: "",
  };
}

const snapshot: Snapshot = {
  methods: [
    { id: "hooks", name: "Hooks", vendor: "Anthropic", plans: "", facts: {} },
  ],
  products: [
    {
      id: "claude-code-cli",
      name: "Claude Code · CLI",
      vendor: "Anthropic",
      family: "Claude Code",
      surface: "CLI",
    },
    {
      id: "cursor-ide",
      name: "Cursor · IDE",
      vendor: "Cursor",
      family: "Cursor",
      surface: "IDE",
    },
  ],
  capabilities: [
    { id: "session", name: "Session tracking", group: "Observability" },
  ],
  draft: {
    mappings: {
      "hooks/claude-code-cli": {
        applicability: "applicable",
        conditions: "",
        facts: { session: { status: "supported", note: "", verify: false } },
      },
      "hooks/cursor-ide": {
        applicability: "applicable",
        conditions: "",
        facts: { session: { status: "supported", note: "", verify: false } },
      },
    },
    references: {},
  },
  revision: "r1",
};

describe("coverage model", () => {
  it("folds catalog product families onto surfaces", () => {
    const [method] = methodFootprints(snapshot, new Map(), true);

    expect(method?.footprint).toContain(cellKey("session", "claude_code"));
    expect(method?.footprint).toContain(cellKey("session", "cursor"));
  });

  it("counts only unobserved claims as gaps", () => {
    const cells = indexCells([cell("session", "claude_code", "observed")]);
    const [method] = methodFootprints(snapshot, cells, true);

    expect(method?.footprint.size).toBe(2);
    expect(method?.gaps.size).toBe(1);
    expect(method?.gaps.has(cellKey("session", "cursor"))).toBe(true);
  });

  it("leaves methods unranked until coverage has loaded", () => {
    // Against an empty map every claim looks like a gap, so ranking then would
    // order by claim count rather than by what this org is missing.
    const unranked = methodFootprints(snapshot, new Map(), false);
    expect(unranked).toHaveLength(1);
    expect(unranked[0]?.gaps.size).toBe(2);
  });

  describe("Claude families", () => {
    const supported = { status: "supported", note: "", verify: false } as const;
    const product = (id: string, family: string) => ({
      id,
      name: `${family} · Web`,
      vendor: "Anthropic",
      family,
      surface: "Web",
    });
    // Each method maps to exactly one product, so each footprint isolates one
    // family's routing.
    const routes: Record<string, string> = {
      plugins: "claude-unified-chat-web",
      "anthropic-api": "claude-unified-chat-web",
      inference: "claude-unified-chat-web",
      "legacy-chat": "claude-chat-web",
      "legacy-cowork": "cowork-web",
    };
    const families: Snapshot = {
      ...snapshot,
      methods: Object.keys(routes).map((id) => ({
        id,
        name: id,
        vendor: "Anthropic",
        plans: "",
        facts: {},
      })),
      products: [
        product("claude-unified-chat-web", "Claude · Unified Chat"),
        product("claude-chat-web", "Claude Chat (deprecated)"),
        product("cowork-web", "Cowork (deprecated)"),
      ],
      draft: {
        mappings: Object.fromEntries(
          Object.entries(routes).map(([method, productId]) => [
            `${method}/${productId}`,
            {
              applicability: "applicable" as const,
              conditions: "",
              facts: { session: supported },
            },
          ]),
        ),
        references: {},
      },
    };
    const footprintOf = (methodId: string) => [
      ...(methodFootprints(families, new Map(), false).find(
        (m) => m.id === methodId,
      )?.footprint ?? []),
    ];

    it.each([
      // The plugin's hooks report from the Cowork VM.
      ["plugins", "cowork"],
      // The Compliance API and inference hooks import conversations as chat.
      ["anthropic-api", "claude_chat"],
      ["inference", "claude_chat"],
      // Deprecated families keep the columns they had before the rename.
      ["legacy-chat", "claude_chat"],
      ["legacy-cowork", "cowork"],
    ] as const)("folds %s onto %s", (methodId, surface) => {
      expect(footprintOf(methodId)).toEqual([cellKey("session", surface)]);
    });
  });

  it("omits methods that claim nothing", () => {
    const empty: Snapshot = {
      ...snapshot,
      draft: { mappings: {}, references: {} },
    };
    expect(methodFootprints(empty, new Map(), true)).toHaveLength(0);
  });
});
