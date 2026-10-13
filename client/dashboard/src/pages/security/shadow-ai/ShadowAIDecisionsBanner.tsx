import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { pluralize } from "@/lib/format";
import { useRoutes } from "@/routes";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { Link } from "react-router";
import { findBlockingShadowMCPPolicy, isShadowAIPolicy } from "../policy-type";
import { useUndecidedToolCount } from "./use-undecided-tool-count";

/** "1 harness or assistant", "6 harnesses or assistants": both nouns take the
 *  plural, which pluralize() alone would give only the last one. */
function aiToolsPhrase(count: number): string {
  return `${count} ${count === 1 ? "harness or assistant" : "harnesses or assistants"}`;
}

/** Where the banner sends the reader. The project's blocking Shadow MCP policy
 *  is the Shadow AI policy only when it runs no other detector; a mixed one
 *  opens in the standard editor, which has no decisions list, so the banner
 *  says so instead of pointing at a page that cannot help. */
type BannerTarget =
  | { kind: "create" }
  | { kind: "policy"; policy: RiskPolicy }
  | { kind: "mixed"; policy: RiskPolicy };

function bannerTarget(policies: readonly RiskPolicy[]): BannerTarget {
  const policy = findBlockingShadowMCPPolicy(policies);
  if (!policy) return { kind: "create" };
  return isShadowAIPolicy(policy)
    ? { kind: "policy", policy }
    : { kind: "mixed", policy };
}

function postureSentence(target: BannerTarget): string {
  switch (target.kind) {
    case "create":
      return "Create the Shadow AI policy to start deciding.";
    case "mixed":
      return "The blocking Shadow MCP policy also runs other detectors, so it opens in the standard editor. Give the Shadow MCP detector a policy of its own to review tool decisions here.";
    case "policy":
      return target.policy.shadowMcpDisposition === "allow_all"
        ? "Unreviewed tools stay allowed until you disallow them."
        : "Unreviewed MCP servers are blocked by the policy default; unreviewed harnesses still connect.";
  }
}

function ctaLabel(target: BannerTarget): string {
  switch (target.kind) {
    case "create":
      return "Set up Shadow AI";
    case "mixed":
      return "Open policy";
    case "policy":
      return "Review decisions";
  }
}

/** The Guardrails banner for the Shadow AI backlog: how many detected tools
 *  and servers have no decision, and the way to record one. A failed read
 *  shows nothing rather than a zero that would pass for "nothing to do". */
function UndecidedToolsBanner({
  policies,
}: {
  policies: readonly RiskPolicy[];
}): JSX.Element | null {
  const routes = useRoutes();
  const undecided = useUndecidedToolCount();
  if (undecided.isError || undecided.total === 0) return null;
  const target = bannerTarget(policies);
  const href =
    target.kind === "create"
      ? `${routes.policyCenter.new.href()}?kind=shadow-ai`
      : routes.policyCenter.detail.href(target.policy.id);
  return (
    <Alert variant="warning" alignTop className="mb-4">
      <div className="flex w-full flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <AlertTitle>
            {undecided.truncated ? "At least " : ""}
            {pluralize(undecided.total, "AI tool")} without a decision in Shadow
            AI
          </AlertTitle>
          <AlertDescription>
            {aiToolsPhrase(undecided.aiTools)},{" "}
            {pluralize(undecided.mcpServers, "MCP server")}
            {undecided.truncated ? " on the first inventory page" : ""}.{" "}
            {postureSentence(target)}
          </AlertDescription>
        </div>
        <Button asChild variant="secondary" size="sm">
          <Link to={href}>{ctaLabel(target)}</Link>
        </Button>
      </div>
    </Alert>
  );
}

/** Mounts the banner only while the Shadow AI fold-in is on, so its reads
 *  run only where it is shown. */
export function ShadowAIDecisionsBanner({
  policies,
}: {
  policies: readonly RiskPolicy[];
}): JSX.Element | null {
  const enabled =
    useFeatureFlag(FEATURE_FLAGS.shadowAiGuardrails).status === "enabled";
  if (!enabled) return null;
  return <UndecidedToolsBanner policies={policies} />;
}
