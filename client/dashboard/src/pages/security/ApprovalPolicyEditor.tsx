import { Card } from "@/components/ui/Card";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskPoliciesGet } from "@gram/client/react-query/riskPoliciesGet.js";
import { useRiskPoliciesUpdateMutation } from "@gram/client/react-query/riskPoliciesUpdate.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AudiencePrincipalRow,
  AudiencePrincipalSection,
  PolicyAudiencePicker,
  SpecificUsersAudienceSection,
} from "./PolicyCenter";
import {
  compareMembersByName,
  compareRolesByName,
  memberDisplayName,
} from "./audience-principals";
import { PolicyMCPScopePicker } from "./PolicyMCPScopePicker";
import {
  PolicyHeader,
  SectionHeader,
  StepNav,
  StepperShell,
  SummaryRow,
} from "./policy-editor-shell";
import {
  type PolicyMCPScopeValue,
  policyMCPScopeComplete,
  policyMCPScopePayload,
  policyMCPScopeValue,
  policyScopeSummary,
} from "./policy-mcp-scope";
import { type PolicyStep, useStepParam } from "./use-step-param";

const APPROVAL_STEPS: PolicyStep[] = [
  {
    id: "tools",
    title: "Tools",
    description: "Choose the MCP servers and tools whose calls need approval.",
  },
  {
    id: "who",
    title: "Who",
    description: "Choose whose calls are held, and who may approve them.",
  },
  {
    id: "review",
    title: "Review",
    description: "Confirm the configuration before creating the policy.",
  },
];

const APPROVAL_DEFAULT_NAME = "Tool approval";

type AudienceType = "everyone" | "targeted";

function audienceTypeOf(policy: RiskPolicy | null): AudienceType {
  return policy?.audienceType === "targeted" ? "targeted" : "everyone";
}

function audiencePrincipalsOf(policy: RiskPolicy | null): Set<string> {
  return policy?.audienceType === "targeted"
    ? new Set(policy.audiencePrincipalUrns ?? [])
    : new Set<string>();
}

function approversOf(policy: RiskPolicy | null): Set<string> {
  return new Set(policy?.approvalConfig?.approvers ?? []);
}

/** An approval policy always holds MCP tool calls, so a new one starts on a
 *  server scope with nothing picked yet. */
function initialScope(policy: RiskPolicy | null): PolicyMCPScopeValue {
  if (policy?.mcpScope) return policyMCPScopeValue(policy.mcpScope);
  return { mode: "mcp", allServers: false, toolAnnotations: [], servers: [] };
}

function sameSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const value of a) if (!b.has(value)) return false;
  return true;
}

/** A readable name for a user or role principal, falling back to the URN
 *  when the member or role is not loaded. */
function principalLabel(
  urn: string,
  members: AccessMember[],
  roles: Role[],
): string {
  const member = members.find((candidate) => candidate.principalUrn === urn);
  if (member) return memberDisplayName(member);
  const role = roles.find((candidate) => candidate.principalUrn === urn);
  if (role) return `${role.name} (role)`;
  return urn;
}

/** Approvers are specific people or roles, never everyone: an approval that
 *  anyone can give is no approval. Users and roles can be mixed. */
function ApproversPicker({
  approvers,
  setApprovers,
  members,
  roles,
}: {
  approvers: Set<string>;
  setApprovers: (value: Set<string>) => void;
  members: AccessMember[];
  roles: Role[];
}): JSX.Element {
  const [userSearch, setUserSearch] = useState("");
  const toggle = (principalUrn: string, checked: boolean) => {
    const next = new Set(approvers);
    if (checked) {
      next.add(principalUrn);
    } else {
      next.delete(principalUrn);
    }
    setApprovers(next);
  };

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <Label className="text-sm font-medium">Approvers</Label>
        <p className="text-muted-foreground text-xs">
          Who may approve a held call. Pick people, roles or both. The person
          who made a call can never approve it.
        </p>
      </div>
      <SpecificUsersAudienceSection
        members={members}
        userSearch={userSearch}
        setUserSearch={setUserSearch}
        selectedAudiencePrincipalUrns={approvers}
        onTogglePrincipal={toggle}
      />
      <div className="border-border border">
        <AudiencePrincipalSection title="Roles">
          {roles.length === 0 ? (
            <p className="text-muted-foreground px-4 py-3 text-sm">
              No roles available.
            </p>
          ) : (
            roles.map((role) => (
              <AudiencePrincipalRow
                key={role.principalUrn}
                id={`approver-${role.principalUrn}`}
                checked={approvers.has(role.principalUrn)}
                title={role.name}
                subtitle={`${role.memberCount} members`}
                onCheckedChange={(checked) =>
                  toggle(role.principalUrn, checked)
                }
              />
            ))
          )}
        </AudiencePrincipalSection>
      </div>
      {approvers.size === 0 && (
        <Text small muted>
          Select at least one approver.
        </Text>
      )}
    </div>
  );
}

function ApprovalReview({
  name,
  mcpScope,
  audienceType,
  audienceLabels,
  approverLabels,
}: {
  name: string;
  mcpScope: PolicyMCPScopeValue;
  audienceType: AudienceType;
  audienceLabels: string[];
  approverLabels: string[];
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description="Confirm the configuration before creating the policy." />
      <Stack gap={4}>
        <SummaryRow label="Name">
          <Text small>{name.trim() || APPROVAL_DEFAULT_NAME}</Text>
        </SummaryRow>
        <SummaryRow label="Tools">
          <Text small>{policyScopeSummary(mcpScope)}</Text>
        </SummaryRow>
        <SummaryRow label="Action">
          <Text small>Hold the call until an approver approves it</Text>
        </SummaryRow>
        <SummaryRow label="Applies to">
          <Text small className="text-right">
            {audienceType === "everyone"
              ? "Everyone"
              : audienceLabels.join(", ")}
          </Text>
        </SummaryRow>
        <SummaryRow label="Approvers">
          <Text small className="text-right">
            {approverLabels.join(", ")}
          </Text>
        </SummaryRow>
        <Text small muted>
          A held call returns a request token. The agent files the request,
          waits for a decision, and retries once approved. Each approval lets
          one retry of the exact call through.
        </Text>
      </Stack>
    </Card>
  );
}

/**
 * Editor for a tool approval policy: a require_approval policy that holds
 * matching MCP tool calls until a listed approver approves them. Three steps,
 * Tools → Who → Review. It has no detectors and no block message, so it does
 * not share the standard editor's Detect and Action steps.
 */
export function ApprovalPolicyEditor({
  policy,
}: {
  policy: RiskPolicy | null;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [step, setStep] = useStepParam(APPROVAL_STEPS);

  const [name, setName] = useState(policy?.name ?? "");
  const [mcpScope, setMcpScope] = useState<PolicyMCPScopeValue>(() =>
    initialScope(policy),
  );
  const [audienceType, setAudienceType] = useState<AudienceType>(() =>
    audienceTypeOf(policy),
  );
  const [audiencePrincipalUrns, setAudiencePrincipalUrns] = useState<
    Set<string>
  >(() => audiencePrincipalsOf(policy));
  const [approvers, setApprovers] = useState<Set<string>>(() =>
    approversOf(policy),
  );

  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roles = useMemo(
    () => [...(rolesData?.roles ?? [])].sort(compareRolesByName),
    [rolesData?.roles],
  );
  const members = useMemo(
    () => [...(membersData?.members ?? [])].sort(compareMembersByName),
    [membersData?.members],
  );

  const mcpScopePayload = policyMCPScopePayload(mcpScope);
  const storedScope = policy
    ? policyMCPScopePayload(initialScope(policy))
    : null;
  const dirty =
    policy !== null &&
    (name.trim() !== policy.name ||
      JSON.stringify(mcpScopePayload) !== JSON.stringify(storedScope) ||
      audienceType !== audienceTypeOf(policy) ||
      !sameSet(audiencePrincipalUrns, audiencePrincipalsOf(policy)) ||
      !sameSet(approvers, approversOf(policy)));

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskListPolicies(queryClient);
      toast.success("Policy updated");
    },
    onError: () => toast.error("The policy could not be saved. Try again."),
  });
  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: (created) => {
      void invalidateAllRiskListPolicies(queryClient);
      toast.success("Approval policy created", {
        description:
          "Agents it applies to now ask for approval before calling these tools.",
      });
      routes.policyCenter.detail.goTo(created.id);
    },
    onError: () => toast.error("The policy could not be created. Try again."),
  });
  const saving = updateMutation.isPending || createMutation.isPending;

  const scopeComplete = policyMCPScopeComplete(mcpScope);
  const audienceComplete =
    audienceType === "everyone" || audiencePrincipalUrns.size > 0;
  const canSave = scopeComplete && audienceComplete && approvers.size > 0;
  const trimmedName = name.trim() || APPROVAL_DEFAULT_NAME;
  const principals =
    audienceType === "targeted" ? [...audiencePrincipalUrns] : [];

  const save = () => {
    if (!mcpScopePayload) return;
    const approvalConfig = { approvers: [...approvers].sort() };
    if (policy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: policy.id,
            name: trimmedName,
            action: "require_approval",
            mcpScope: mcpScopePayload,
            approvalConfig,
            audienceType,
            audiencePrincipalUrns: principals,
            autoName: false,
          },
        },
      });
      return;
    }
    createMutation.mutate({
      request: {
        createRiskPolicyRequestBody: {
          name: trimmedName,
          enabled: true,
          action: "require_approval",
          sources: [],
          mcpScope: mcpScopePayload,
          approvalConfig,
          audienceType,
          audiencePrincipalUrns: principals,
          autoName: false,
        },
      },
    });
  };

  const header = (
    <PolicyHeader
      kind="approval"
      policy={policy}
      name={name}
      onNameChange={setName}
      dirty={dirty}
      saving={saving}
      actionDisabled={!canSave}
      onSubmit={save}
      onCreate={save}
    />
  );

  return (
    <StepperShell
      header={header}
      steps={APPROVAL_STEPS}
      current={step}
      onStep={setStep}
    >
      {step === 0 && (
        <Card>
          <SectionHeader description="Calls to these tools are held until an approver approves them." />
          <PolicyMCPScopePicker
            value={mcpScope}
            onChange={(next) => setMcpScope({ ...next, mode: "mcp" })}
            action="require_approval"
          />
        </Card>
      )}
      {step === 1 && (
        <div className="space-y-4">
          <Card>
            <SectionHeader description="Whose calls are held. Agents and people outside it call these tools as usual." />
            <PolicyAudiencePicker
              formAudienceType={audienceType}
              setFormAudienceType={setAudienceType}
              selectedAudiencePrincipalUrns={audiencePrincipalUrns}
              setSelectedAudiencePrincipalUrns={setAudiencePrincipalUrns}
            />
          </Card>
          <Card>
            <ApproversPicker
              approvers={approvers}
              setApprovers={setApprovers}
              members={members}
              roles={roles}
            />
          </Card>
        </div>
      )}
      {step === 2 && (
        <ApprovalReview
          name={name}
          mcpScope={mcpScope}
          audienceType={audienceType}
          audienceLabels={principals.map((urn) =>
            principalLabel(urn, members, roles),
          )}
          approverLabels={[...approvers].map((urn) =>
            principalLabel(urn, members, roles),
          )}
        />
      )}
      <StepNav
        step={step}
        count={APPROVAL_STEPS.length}
        onStep={setStep}
        continueDisabled={
          (step === 0 && !scopeComplete) ||
          (step === 1 && (!audienceComplete || approvers.size === 0))
        }
      />
    </StepperShell>
  );
}
