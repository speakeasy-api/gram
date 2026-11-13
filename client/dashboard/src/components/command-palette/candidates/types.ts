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
  | "shadow_mcp_server"
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
