import { audienceGroups } from "@/components/mcp-approvals/audienceGroups";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useState } from "react";
import { EditShadowMCPPolicyDialog } from "./EditShadowMCPPolicyDialog";
import { shadowMCPChosenAudience, shadowMCPModeOf } from "./shadowMCPMode";
import { shadowMCPPolicySentence } from "./shadowMCPPolicyChange";

/**
 * The project's Shadow MCP policy in one line, with the one action that
 * changes it. The policy changes rarely and touches every server, so it
 * reads as a statement with an "Edit policy" button rather than a toggle
 * that looks like a filter on the table below.
 */
export function ShadowMCPPolicySummary(): JSX.Element | null {
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  const membersQuery = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  const [editing, setEditing] = useState(false);

  // A failed policy read is reported by the table below, which pauses
  // deciding; an unread list is not proof of either mode, so nothing is said.
  if (policiesQuery.isError) return null;
  if (!policiesQuery.data) {
    return (
      <div className="border-border flex items-center justify-between gap-3 border px-4 py-3">
        <Skeleton className="h-4 w-72" />
        <Skeleton className="h-8 w-24" />
      </div>
    );
  }

  const state = shadowMCPModeOf(policiesQuery.data.policies);
  const targeted =
    state.mode === "blocked" && state.policy?.audienceType === "targeted";
  const urns = targeted ? (state.policy?.audiencePrincipalUrns ?? []) : [];
  // null: everyone. "": a targeted rule that names nobody.
  const narrowedTo = !targeted
    ? null
    : urns.length === 0
      ? ""
      : shadowMCPChosenAudience(
          urns,
          audienceGroups(
            membersQuery.data?.members ?? [],
            rolesQuery.data?.roles ?? [],
          ),
        );
  const heldReason = heldByReason(state.heldBy, state.policy?.name);

  return (
    <div className="border-border flex flex-wrap items-center justify-between gap-3 border px-4 py-3">
      <div className="flex min-w-0 items-center gap-2.5">
        <Icon
          name={state.mode === "blocked" ? "shield-x" : "shield-check"}
          className="text-muted-foreground size-4 shrink-0"
          aria-hidden
        />
        <Text variant="small" className="font-medium">
          {shadowMCPPolicySentence(state.mode, narrowedTo)}
        </Text>
        {heldReason ? (
          <Text muted small>
            Set in Guardrails.
          </Text>
        ) : null}
      </div>
      {heldReason ? (
        <SimpleTooltip tooltip={heldReason}>
          <span tabIndex={0} className="inline-flex">
            <Button variant="secondary" size="sm" disabled>
              <Button.Text>Edit policy</Button.Text>
            </Button>
          </span>
        </SimpleTooltip>
      ) : (
        <Button variant="secondary" size="sm" onClick={() => setEditing(true)}>
          <Button.Text>Edit policy</Button.Text>
        </Button>
      )}
      {heldReason ? null : (
        <EditShadowMCPPolicyDialog
          open={editing}
          onOpenChange={setEditing}
          state={state}
        />
      )}
    </div>
  );
}

function heldByReason(
  heldBy: "shadow_ai" | "guardrails" | null,
  policyName: string | undefined,
): string | null {
  if (heldBy === "shadow_ai") {
    return "Your Shadow AI policy in Guardrails allows servers by default. Change it there.";
  }
  if (heldBy === "guardrails") {
    return `Your ${policyName ?? "Guardrails"} policy sets this because it also scans for other risks. Change it in Guardrails.`;
  }
  return null;
}
