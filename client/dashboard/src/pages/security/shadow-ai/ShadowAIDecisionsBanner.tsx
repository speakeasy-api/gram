import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { pluralize } from "@/lib/format";
import { useRoutes } from "@/routes";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { Link } from "react-router";
import { findBlockingShadowMCPPolicy } from "../policy-type";
import { useUndecidedToolCount } from "./use-undecided-tool-count";

/** "1 harness or assistant", "6 harnesses or assistants": both nouns take the
 *  plural, which pluralize() alone would give only the last one. */
function aiToolsPhrase(count: number): string {
  return `${count} ${count === 1 ? "harness or assistant" : "harnesses or assistants"}`;
}

function postureSentence(policy: RiskPolicy | undefined): string {
  if (!policy) return "Create the Shadow AI policy to start deciding.";
  if (policy.shadowMcpDisposition === "allow_all") {
    return "Unreviewed tools stay allowed until you disallow them.";
  }
  return "Unreviewed MCP servers are blocked by the policy default; unreviewed harnesses still connect.";
}

/** The Guardrails banner for the Shadow AI backlog: how many detected tools
 *  and servers have no decision, and the way to record one. */
function UndecidedToolsBanner({
  policies,
}: {
  policies: readonly RiskPolicy[];
}): JSX.Element | null {
  const routes = useRoutes();
  const undecided = useUndecidedToolCount();
  if (undecided.total === 0) return null;
  const policy = findBlockingShadowMCPPolicy(policies);
  const href = policy
    ? routes.policyCenter.detail.href(policy.id)
    : `${routes.policyCenter.new.href()}?kind=shadow-ai`;
  return (
    <Alert variant="warning" alignTop className="mb-4">
      <div className="flex w-full flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <AlertTitle>
            {pluralize(undecided.total, "AI tool")} without a decision in Shadow
            AI
          </AlertTitle>
          <AlertDescription>
            {aiToolsPhrase(undecided.aiTools)},{" "}
            {pluralize(undecided.mcpServers, "MCP server")}.{" "}
            {postureSentence(policy)}
          </AlertDescription>
        </div>
        <Button asChild variant="secondary" size="sm">
          <Link to={href}>
            {policy ? "Review decisions" : "Set up Shadow AI"}
          </Link>
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
