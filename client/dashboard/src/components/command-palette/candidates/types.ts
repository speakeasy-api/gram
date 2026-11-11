import type { IconName } from "@/components/ui/Icon/names";
import type { ReleaseStage } from "@/components/release-stage-badge";

export type Verb = "open" | "enable" | "disable" | "publish";

type CandidateKind =
  | "page"
  | "recent"
  | "mcp_server"
  | "gateway"
  | "catalog"
  | "plugin"
  | "marketplace"
  | "skill"
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
  title: string;
  detail: string;
  /** Slug, id, and aliases used for fuzzy matching. */
  keywords: string[];
  /** "open" always first; others attached by state + RBAC. */
  verbs: Verb[];
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

/**
 * Verbs the palette confirms before running, because they cannot be undone.
 * Reversible verbs run on the first Enter and offer an Undo in their toast.
 */
export const CONFIRM_VERBS: ReadonlySet<Verb> = new Set<Verb>(["publish"]);

/**
 * Verbs that change state. Reversible ones run on the first Enter and toast
 * an Undo; irreversible ones confirm first — see {@link CONFIRM_VERBS}.
 */
export const MUTATING_VERBS: ReadonlySet<Verb> = new Set<Verb>([
  "enable",
  "disable",
  "publish",
]);
