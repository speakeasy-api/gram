/**
 * Prototype data model for the Redaction Rules page.
 *
 * A redaction rule strips or masks sensitive fields from telemetry for a
 * specific individual before events are stored. Rules are matched at ingest
 * against the identity attributes on each event (user.email, session owner)
 * and apply to every project in the organization or to a single project.
 * The page is a UI prototype: static fixtures and local state only — no
 * backend yet.
 */

export const REDACTION_TARGETS = [
  "log_bodies",
  "prompt_text",
  "tool_io",
  "identity",
] as const;

export type RedactionTarget = (typeof REDACTION_TARGETS)[number];

export const TARGET_LABELS: Record<RedactionTarget, string> = {
  log_bodies: "Log bodies",
  prompt_text: "Prompt text",
  tool_io: "Tool inputs & outputs",
  identity: "User identity",
};

export const TARGET_DESCRIPTIONS: Record<RedactionTarget, string> = {
  log_bodies: "Replace the raw body of log events with a redaction marker.",
  prompt_text: "Strip user prompt content from prompt and chat events.",
  tool_io: "Drop tool call inputs and outputs, keeping only tool names.",
  identity: "Pseudonymize identity attributes (email, name) on every event.",
};

export interface RedactionRule {
  id: string;
  /** The individual the rule protects; matched on identity attributes. */
  subjectName: string;
  subjectEmail: string;
  targets: RedactionTarget[];
  /** Project the rule is scoped to, or null for every project in the org. */
  project: string | null;
  enabled: boolean;
  createdAt: Date;
}

export function buildMockRules(now: Date = new Date()): RedactionRule[] {
  const daysAgo = (days: number) => new Date(now.getTime() - days * 86_400_000);

  return [
    {
      id: "rr_001",
      subjectName: "Ada Lovelace",
      subjectEmail: "ada@example.com",
      targets: ["prompt_text", "tool_io"],
      project: null,
      enabled: true,
      createdAt: daysAgo(12),
    },
    {
      id: "rr_002",
      subjectName: "Grace Hopper",
      subjectEmail: "grace@example.com",
      targets: ["identity"],
      project: "internal-tools",
      enabled: true,
      createdAt: daysAgo(30),
    },
    {
      id: "rr_003",
      subjectName: "Alan Turing",
      subjectEmail: "alan@example.com",
      targets: ["log_bodies", "prompt_text"],
      project: "default",
      enabled: false,
      createdAt: daysAgo(45),
    },
  ];
}
