import { audienceGroups } from "@/components/mcp-approvals/audienceGroups";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Icon } from "@/components/ui/Icon";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import {
  invalidateAllRiskListPolicies,
  useRiskListPolicies,
} from "@gram/client/react-query/riskListPolicies.js";
import { useRiskPoliciesDeleteMutation } from "@gram/client/react-query/riskPoliciesDelete.js";
import { useRiskPoliciesUpdateMutation } from "@gram/client/react-query/riskPoliciesUpdate.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { invalidateAllShadowMCPInventoryServer } from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY,
  type ShadowMCPMode,
  shadowMCPAudienceSummary,
  shadowMCPModeOf,
} from "./shadowMCPMode";

/** What the dialog is about to do: switch the mode, or change who
 *  "blocked by default" applies to. */
type PendingChange = ShadowMCPMode | "audience";

const COPY: Record<
  PendingChange,
  { title: string; description: string; confirm: string; done: string }
> = {
  blocked: {
    title: "Block servers by default?",
    description:
      "Every MCP server nobody approved is blocked, including servers people use today. Approve a server to let it through.",
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
  audience: {
    title: "Who is blocked by default?",
    description:
      "Servers nobody approved are blocked for these people. Everyone else is allowed by default.",
    confirm: "Save",
    done: "Saved who is blocked by default.",
  },
};

/**
 * The one setting the Shadow MCP page needs: what happens to a server nobody
 * has decided on. Allowed by default is built in and needs no policy;
 * blocked by default is a policy created, edited and removed here, never in
 * Guardrails. By default it applies to everyone; under Advanced it can apply
 * to chosen people or roles instead.
 */
export function ShadowMCPModeControl(): JSX.Element | null {
  const queryClient = useQueryClient();
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  const membersQuery = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  const create = useRiskCreatePolicyMutation();
  const update = useRiskPoliciesUpdateMutation();
  const remove = useRiskPoliciesDeleteMutation();
  const [pending, setPending] = useState<PendingChange | null>(null);
  const [principals, setPrincipals] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  // An unread policy list is not proof of either mode, so nothing is shown
  // rather than a mode the project may not be in.
  if (!policiesQuery.data) return null;
  const state = shadowMCPModeOf(policiesQuery.data.policies);
  const saving = create.isPending || update.isPending || remove.isPending;
  const groups = audienceGroups(
    membersQuery.data?.members ?? [],
    rolesQuery.data?.roles ?? [],
  );
  const currentPrincipals =
    state.policy?.audienceType === "targeted"
      ? state.policy.audiencePrincipalUrns
      : [];

  const open = (change: PendingChange) => {
    setError(null);
    setPrincipals(change === "audience" ? currentPrincipals : []);
    setPending(change);
  };

  const audience = {
    audienceType: principals.length > 0 ? "targeted" : "everyone",
    audiencePrincipalUrns: principals,
  } as const;

  const apply = async (change: PendingChange) => {
    setError(null);
    try {
      if (change === "blocked") {
        await create.mutateAsync({
          request: {
            createRiskPolicyRequestBody: {
              ...SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY,
              sources: [...SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY.sources],
              ...audience,
              audiencePrincipalUrns: [...principals],
            },
          },
        });
      }
      if (change === "audience" && state.policy) {
        await update.mutateAsync({
          request: {
            updateRiskPolicyRequestBody: {
              id: state.policy.id,
              name: state.policy.name,
              sources: state.policy.sources,
              action: "block",
              ...audience,
              audiencePrincipalUrns: [...principals],
            },
          },
        });
      }
      if (change === "allowed" && state.policy) {
        await remove.mutateAsync({ request: { id: state.policy.id } });
      }
      await Promise.all([
        invalidateAllRiskListPolicies(queryClient),
        invalidateAllShadowMCPInventory(queryClient),
        invalidateAllShadowMCPInventoryServer(queryClient),
      ]);
      setPending(null);
      toast.success(COPY[change].done);
    } catch {
      setError("The setting could not be changed. Try again.");
    }
  };

  const copy = pending ? COPY[pending] : null;
  const picksAudience = pending === "blocked" || pending === "audience";
  return (
    <div className="flex flex-col items-end gap-1.5">
      <SegmentedControl<ShadowMCPMode>
        ariaLabel="MCP servers nobody decided on"
        value={state.mode}
        busy={saving}
        disabled={saving}
        onChange={(next) => {
          if (next === state.mode) return;
          open(next);
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
      {state.mode === "blocked" ? (
        <div className="flex items-center gap-1">
          <Text muted small>
            {shadowMCPAudienceSummary(state.policy, groups)}
          </Text>
          <Button
            variant="tertiary"
            size="sm"
            disabled={saving}
            onClick={() => open("audience")}
          >
            <Button.Text>Edit</Button.Text>
          </Button>
        </div>
      ) : null}
      <ConfirmDialog
        open={copy !== null}
        onOpenChange={(isOpen) => {
          if (!isOpen && !saving) setPending(null);
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
      >
        {picksAudience ? (
          <AudiencePicker
            groups={groups}
            value={principals}
            onChange={setPrincipals}
            openByDefault={pending === "audience"}
          />
        ) : null}
      </ConfirmDialog>
    </div>
  );
}

/** Who "blocked by default" applies to. Everyone unless the admin opens
 *  Advanced and picks people or roles. */
function AudiencePicker({
  groups,
  value,
  onChange,
  openByDefault,
}: {
  groups: ReturnType<typeof audienceGroups>;
  value: string[];
  onChange: (next: string[]) => void;
  openByDefault: boolean;
}) {
  const picker = (
    <div className="space-y-1.5">
      <Text variant="small" className="font-medium">
        Who it applies to
      </Text>
      <MultiSelect
        options={groups}
        defaultValue={value}
        onValueChange={onChange}
        placeholder="Everyone in the project"
      />
      <Text muted small>
        Pick people or roles to block by default only for them. Leave empty for
        everyone.
      </Text>
    </div>
  );
  if (openByDefault) return picker;
  return (
    <Collapsible>
      <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
        <Icon name="chevron-right" className="size-3.5" aria-hidden />
        Advanced
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-3">{picker}</CollapsibleContent>
    </Collapsible>
  );
}
