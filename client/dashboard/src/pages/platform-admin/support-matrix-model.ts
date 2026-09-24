import type {
  Capability,
  SupportCoverageCell,
  SupportCoverageCellSurface,
} from "@gram/client/models/components/supportcoveragecell.js";

export type SurfaceId = SupportCoverageCellSurface;
export type CapabilityId = Capability;

/**
 * Column and row labels for the matrix. Cells are looked up by (capability,
 * surface), so a reordering on either side cannot shift a value into the wrong
 * column. Folding hook_source onto a surface is server-side, in
 * internal/agentsurface.
 */
export const surfaces: ReadonlyArray<{
  id: SurfaceId;
  name: string;
  detail: string;
}> = [
  { id: "claude_code", name: "Claude Code", detail: "CLI · Desktop · Cloud" },
  { id: "claude_chat", name: "Claude Chat", detail: "Web · Desktop" },
  { id: "cowork", name: "Cowork", detail: "Desktop · Cloud · Web" },
  { id: "codex", name: "Codex / ChatGPT", detail: "CLI · Desktop · Web" },
  { id: "cursor", name: "Cursor", detail: "IDE · CLI · Cloud" },
  { id: "other", name: "Other agents", detail: "OpenCode · Gemini · Copilot" },
];

export const capabilities: ReadonlyArray<{
  id: CapabilityId;
  name: string;
  description: string;
  /** Noun for the cell's primary measure, pluralized by the caller. */
  unit: string;
}> = [
  {
    id: "session",
    name: "Session activity observed",
    description: "Aggregate chat-session activity",
    unit: "session",
  },
  {
    id: "blocking",
    name: "Policy enforcement",
    description: "Synchronous policy-decision evidence",
    unit: "block",
  },
  {
    id: "identity",
    name: "Identity attribution",
    description: "Sessions bound to a named user",
    unit: "attributed session",
  },
  {
    id: "cost",
    name: "Token usage observed",
    description: "Aggregate token usage by surface",
    unit: "token",
  },
  {
    id: "shadow",
    name: "Shadow MCP & AI",
    description: "Unsanctioned clients and servers",
    unit: "server",
  },
];

export type IntegrationMethod = {
  id: string;
  name: string;
  description: string;
  setup: string;
  surfaces: SurfaceId[];
  capabilities: CapabilityId[];
};

/**
 * What each integration can reach: static product capability, not observed
 * state. Rendered only against the observed matrix, so a card reads as a
 * recommendation for this org.
 */
export const methods: IntegrationMethod[] = [
  {
    id: "inference-hooks",
    name: "Anthropic Inference Hooks",
    description: "Broad Claude coverage with synchronous policy decisions.",
    setup: "About 10 minutes · Anthropic console",
    surfaces: ["claude_code", "claude_chat", "cowork"],
    capabilities: ["session", "blocking"],
  },
  {
    id: "client-hooks",
    name: "Client hooks",
    description:
      "Local session, tool, and usage evidence from supported developer agents.",
    setup: "About 20 minutes · managed settings",
    surfaces: ["claude_code", "cowork", "codex", "cursor", "other"],
    capabilities: ["session", "blocking", "cost"],
  },
  {
    id: "device-agent",
    name: "Device Agent",
    description:
      "Device-local inventory and activity, including shadow AI and MCP discovery.",
    setup: "1–2 days · fleet rollout",
    surfaces: ["claude_code", "codex", "cursor", "other"],
    capabilities: ["session", "identity", "cost", "shadow"],
  },
  {
    id: "provider-import",
    name: "Provider admin integrations",
    description:
      "Read-only workspace imports for conversations, usage, and spend.",
    setup: "5–15 minutes · provider admin key",
    surfaces: ["claude_chat", "codex", "cursor"],
    capabilities: ["session", "cost", "identity"],
  },
  {
    id: "otel",
    name: "OpenTelemetry export",
    description: "Usage and session evidence through an existing collector.",
    setup: "About 45 minutes · collector config",
    surfaces: ["claude_code", "cowork"],
    capabilities: ["session", "cost"],
  },
];

/** Stable key for one cell of the matrix. */
export function cellKey(
  capability: CapabilityId,
  surface: SurfaceId,
): `${CapabilityId}:${SurfaceId}` {
  return `${capability}:${surface}`;
}

export function indexCells(
  cells: SupportCoverageCell[] | undefined,
): Map<string, SupportCoverageCell> {
  const index = new Map<string, SupportCoverageCell>();
  for (const cell of cells ?? []) {
    index.set(cellKey(cell.capability, cell.surface), cell);
  }
  return index;
}

/**
 * The cells an integration would fill that are not already observed. Ranking
 * on this rather than footprint size keeps a method that covers only existing
 * ground from being recommended.
 */
export function gapsClosedBy(
  method: IntegrationMethod,
  cells: Map<string, SupportCoverageCell>,
): Set<string> {
  const gaps = new Set<string>();
  for (const surface of method.surfaces) {
    for (const capability of method.capabilities) {
      const key = cellKey(capability, surface);
      if (cells.get(key)?.status !== "observed") {
        gaps.add(key);
      }
    }
  }
  return gaps;
}

/** Every cell an integration touches, observed or not. */
export function footprintOf(method: IntegrationMethod): Set<string> {
  const footprint = new Set<string>();
  for (const surface of method.surfaces) {
    for (const capability of method.capabilities) {
      footprint.add(cellKey(capability, surface));
    }
  }
  return footprint;
}

export function activeAgentCoverageLabel(
  attestation: "device" | "user" | undefined,
  activeWindowMinutes: number | undefined,
): string {
  const subject =
    attestation === "device"
      ? "devices running the agent"
      : "devices whose assigned user has an active agent";
  return activeWindowMinutes
    ? `${subject} within ${activeWindowMinutes} minutes`
    : subject;
}
