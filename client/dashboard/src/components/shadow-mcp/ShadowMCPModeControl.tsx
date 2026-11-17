import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  invalidateAllRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { useRiskPoliciesDeleteMutation } from "@gram/client/react-query/riskPoliciesDelete.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { invalidateAllShadowMCPInventoryServer } from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY,
  type ShadowMCPMode,
  shadowMCPModeOf,
} from "./shadowMCPMode";

const MODE_COPY: Record<
  ShadowMCPMode,
  { title: string; description: string; confirm: string; done: string }
> = {
  blocked: {
    title: "Block servers by default?",
    description:
      "Every MCP server nobody approved is blocked for everyone, including servers people use today. Approve a server to let it through.",
    confirm: "Block by default",
    done: "MCP servers are now blocked by default.",
  },
  allowed: {
    title: "Allow servers by default?",
    description:
      "Every MCP server connects unless you deny it. Servers you denied stay blocked, and linked servers still send agents to their official version.",
    confirm: "Allow by default",
    done: "MCP servers are now allowed by default.",
  },
};

/**
 * The one setting the Shadow MCP page needs: what happens to a server nobody
 * has decided on. Allowed by default is built in and needs no policy;
 * blocked by default is a policy created and removed here, never in
 * Guardrails.
 */
export function ShadowMCPModeControl(): JSX.Element | null {
  const queryClient = useQueryClient();
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  const [pending, setPending] = useState<ShadowMCPMode | null>(null);
  const create = useRiskCreatePolicyMutation();
  const remove = useRiskPoliciesDeleteMutation();
  const [error, setError] = useState<string | null>(null);

  // An unread policy list is not proof of either mode, so nothing is shown
  // rather than a mode the project may not be in.
  if (!policiesQuery.data) return null;
  const state = shadowMCPModeOf(policiesQuery.data.policies);
  const saving = create.isPending || remove.isPending;

  const apply = async (next: ShadowMCPMode) => {
    setError(null);
    try {
      if (next === "blocked") {
        await create.mutateAsync({
          request: {
            createRiskPolicyRequestBody: {
              ...SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY,
              sources: [...SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY.sources],
              audiencePrincipalUrns: [],
            },
          },
        });
      }
      if (next === "allowed" && state.policy) {
        await remove.mutateAsync({ request: { id: state.policy.id } });
      }
      await Promise.all([
        invalidateAllRiskListPolicies(queryClient),
        invalidateAllShadowMCPInventory(queryClient),
        invalidateAllShadowMCPInventoryServer(queryClient),
      ]);
      setPending(null);
      toast.success(MODE_COPY[next].done);
    } catch {
      setError("The setting could not be changed. Try again.");
    }
  };

  const copy = pending ? MODE_COPY[pending] : null;
  return (
    <>
      <SegmentedControl<ShadowMCPMode>
        ariaLabel="MCP servers nobody decided on"
        value={state.mode}
        busy={saving}
        disabled={saving}
        onChange={(next) => {
          if (next === state.mode) return;
          setError(null);
          setPending(next);
        }}
        options={[
          {
            value: "allowed",
            label: "Allowed by default",
            tooltip:
              "Servers connect unless you deny them. Linked servers send agents to their official version.",
          },
          {
            value: "blocked",
            label: "Blocked by default",
            disabled: state.heldByShadowAI,
            tooltip: state.heldByShadowAI
              ? "Your Shadow AI policy in Guardrails allows servers by default. Change it there."
              : "Servers are blocked until you approve them.",
          },
        ]}
      />
      <ConfirmDialog
        open={copy !== null}
        onOpenChange={(open) => {
          if (!open && !saving) setPending(null);
        }}
        title={copy?.title ?? ""}
        description={copy?.description ?? ""}
        confirmLabel={copy?.confirm ?? ""}
        pendingLabel="Saving…"
        confirmVariant={
          pending === "blocked" ? "destructive-primary" : "primary"
        }
        isPending={saving}
        error={error}
        onConfirm={() => {
          if (pending) void apply(pending);
        }}
      />
    </>
  );
}
