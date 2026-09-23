import type { IconName } from "@/components/ui/Icon/names";
import type { ReleaseStage } from "@/components/release-stage-badge";

export type Verb = "open" | "enable" | "disable" | "publish";

type CandidateKind =
  | "page"
  | "recent"
  | "mcp_server"
  | "catalog"
  | "plugin"
  | "marketplace"
  | "assistant"
  | "environment"
  | "source"
  | "deployment"
  | "policy"
  | "rule"
  | "access_request"
  | "person"
  | "project";

export interface LauncherCandidate {
  /** Stable across renders: "page:/settings", "mcp:<id>", "marketplace". */
  id: string;
  kind: CandidateKind;
  /** What Jev sees. */
  title: string;
  /** State Jev can read: "MCP server · disabled · 12 tools". */
  detail: string;
  /** Slug, id, aliases — fuzzy only, never sent to Jev. */
  keywords: string[];
  /** "open" always first; others attached by state + RBAC. */
  verbs: Verb[];
  /**
   * Never sent to the intent service; ranks by fuzzy alone. Set on candidates
   * whose title names a person (an identity page in Recents). `person`
   * candidates are excluded by kind regardless.
   */
  fuzzyOnly?: boolean;
  icon?: IconName;
  /** Release stage of a page candidate (pre-GA pages render a badge). */
  stage?: ReleaseStage;
  /** Display heading when there is no judgment ("Pages", "MCP Servers", …). */
  group: string;
  /**
   * Executes the verb. Mutating verbs toast on their own and reject on
   * failure so the palette can return to the list instead of closing.
   */
  run: (verb: Verb) => void | Promise<void>;
}

/** Probability distributions returned by `launcher.judge`. */
export interface Judgment {
  /** Candidate id → probability it is the intended target. */
  target: Record<string, number>;
  /** Verb (or "unclear") → probability it is the intended action. */
  action: Record<string, number>;
  /** Probability the intent is settled enough to run on Enter. */
  ready: number;
}

/**
 * Whether a candidate may be sent to the intent service. People are never
 * sent, by kind or by the `fuzzyOnly` mark; the prefilter and the judge
 * client both apply this so neither can drift from the other.
 */
export function isSendable(c: LauncherCandidate): boolean {
  return c.kind !== "person" && !c.fuzzyOnly;
}

/** Verbs that change state and therefore require a second Enter. */
export const MUTATING_VERBS: ReadonlySet<Verb> = new Set<Verb>([
  "enable",
  "disable",
  "publish",
]);
