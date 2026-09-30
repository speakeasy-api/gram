import {
  emptyMapping,
  getFact,
  mappingKey,
  methodReference,
  type Snapshot,
} from "@/pages/coverage/model";

import type { CapabilityId, CoverageCell, SurfaceId } from "./coverageApi";

export const surfaces: ReadonlyArray<{ id: SurfaceId; name: string }> = [
  { id: "claude_code", name: "Claude Code" },
  { id: "claude_chat", name: "Claude Chat" },
  { id: "cowork", name: "Cowork" },
  { id: "codex", name: "Codex / ChatGPT" },
  { id: "cursor", name: "Cursor" },
  { id: "other", name: "Other agents" },
];

export const capabilities: ReadonlyArray<{
  id: CapabilityId;
  name: string;
  description: string;
  unit: string;
  /** Catalog capability this row is claimed by, when one exists. */
  catalogId?: string;
}> = [
  {
    id: "session",
    name: "Session activity",
    description: "Aggregate chat-session activity",
    unit: "session",
    catalogId: "session",
  },
  {
    id: "blocking",
    name: "Policy enforcement",
    description: "Synchronous policy-decision evidence",
    unit: "block",
    catalogId: "blocking",
  },
  {
    id: "identity",
    name: "Identity attribution",
    description: "Sessions bound to a named user",
    unit: "attributed session",
  },
  {
    id: "cost",
    name: "Token usage",
    description: "Aggregate token usage by surface",
    unit: "token",
    catalogId: "tokens",
  },
  {
    id: "shadow",
    name: "Shadow MCP",
    description: "Unsanctioned servers reached",
    unit: "server",
    catalogId: "shadow-mcp",
  },
];

// The catalog groups products into families that line up with the surfaces the
// telemetry fold produces. Anything outside these falls into "other".
const familyToSurface: Readonly<Record<string, SurfaceId>> = {
  "Claude Code": "claude_code",
  "Claude Chat": "claude_chat",
  Cowork: "cowork",
  Codex: "codex",
  ChatGPT: "codex",
  Cursor: "cursor",
};

export function cellKey(capability: CapabilityId, surface: SurfaceId): string {
  return `${capability}:${surface}`;
}

export function indexCells(
  cells: CoverageCell[] | undefined,
): Map<string, CoverageCell> {
  const index = new Map<string, CoverageCell>();
  for (const cell of cells ?? []) {
    index.set(cellKey(cell.capability, cell.surface), cell);
  }
  return index;
}

export type MethodFootprint = {
  id: string;
  name: string;
  vendor: string;
  /** Cells the method claims it can cover. */
  footprint: Set<string>;
  /** Claimed cells this org has no evidence for. */
  gaps: Set<string>;
};

/**
 * What each integration claims it covers, read from the operator-editable
 * support matrix rather than a second list maintained here.
 *
 * Identity attribution has no catalog capability, so no method claims it.
 */
export function methodFootprints(
  snapshot: Snapshot | undefined,
  cells: Map<string, CoverageCell>,
  ranked: boolean,
): MethodFootprint[] {
  if (!snapshot) return [];

  const rows = snapshot.methods.map((method) => {
    const footprint = new Set<string>();
    for (const product of snapshot.products) {
      const surface = familyToSurface[product.family] ?? "other";
      const mapping =
        snapshot.draft.mappings[mappingKey(method.id, product.id)] ??
        emptyMapping;
      for (const capability of capabilities) {
        if (!capability.catalogId) continue;
        const fact = getFact(
          mapping,
          capability.catalogId,
          methodReference(snapshot.draft, method, capability.catalogId),
        );
        if (fact.status === "supported" || fact.status === "partial") {
          footprint.add(cellKey(capability.id, surface));
        }
      }
    }
    const gaps = new Set(
      [...footprint].filter((key) => cells.get(key)?.status !== "observed"),
    );
    return {
      id: method.id,
      name: method.name,
      vendor: method.vendor,
      footprint,
      gaps,
    };
  });

  const covering = rows.filter((row) => row.footprint.size > 0);
  if (!ranked) return covering;
  return covering.sort((a, b) => b.gaps.size - a.gaps.size);
}
