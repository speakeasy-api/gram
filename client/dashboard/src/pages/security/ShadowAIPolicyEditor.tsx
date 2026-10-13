import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskPoliciesGet } from "@gram/client/react-query/riskPoliciesGet.js";
import { useRiskPoliciesUpdateMutation } from "@gram/client/react-query/riskPoliciesUpdate.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import type { PolicyAction } from "./policy-data";
import {
  ActionStep,
  PolicyHeader,
  SectionHeader,
  StepNav,
  StepperShell,
  SummaryRow,
} from "./policy-editor-shell";
import { type PolicyStep, useStepParam } from "./use-step-param";
import type { ShadowMCPDisposition } from "./policy-shadow-mcp-setup";
import { SeverityBadge } from "./risk-ui";
import { ToolDecisionsSection } from "./shadow-ai/ToolDecisionsSection";

const SHADOW_AI_STEPS: PolicyStep[] = [
  {
    id: "access",
    title: "Access",
    description:
      "Choose how a disallowed tool is refused, and who this applies to.",
  },
  {
    id: "review",
    title: "Review",
    description: "Confirm the configuration before creating the policy.",
  },
];

const SHADOW_AI_DEFAULT_NAME = "Shadow AI";
const SHADOW_AI_DEFAULT_SCORE = 8;

/** The only detector a Shadow AI policy runs. The server counts one enabled
 *  blocking policy with it per project; the chooser points at that one. */
const SHADOW_AI_SOURCES = ["shadow_mcp"];

/** A new Shadow AI policy allows everything it has no decision for, so an
 *  unreviewed MCP server behaves like an unreviewed harness: it connects until
 *  someone disallows it. The disposition cannot change after create. */
const SHADOW_AI_DISPOSITION: ShadowMCPDisposition = "allow_all";

export const SHADOW_AI_ACTION_LOCK_REASON =
  "A Shadow AI policy refuses disallowed tools when they connect. To log Shadow MCP traffic without refusing it, create a Built-in policy with the Shadow MCP detector instead.";

type AudienceType = "everyone" | "targeted";

function audienceTypeOf(policy: RiskPolicy | null): AudienceType {
  return policy?.audienceType === "targeted" ? "targeted" : "everyone";
}

function audiencePrincipalsOf(policy: RiskPolicy | null): Set<string> {
  return policy?.audienceType === "targeted"
    ? new Set(policy.audiencePrincipalUrns ?? [])
    : new Set<string>();
}

function sameSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const value of a) if (!b.has(value)) return false;
  return true;
}

function dispositionLabel(disposition: ShadowMCPDisposition): string {
  return disposition === "allow_all"
    ? "Allowed until disallowed"
    : "MCP servers blocked until allowed";
}

function ShadowAIReview({
  name,
  score,
  audienceType,
  audiencePrincipalCount,
  userMessage,
  disposition,
}: {
  name: string;
  score: number;
  audienceType: AudienceType;
  audiencePrincipalCount: number;
  userMessage: string;
  disposition: ShadowMCPDisposition;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description="Confirm the configuration before creating the policy." />
      <Stack gap={4}>
        <SummaryRow label="Name">
          <Text small>{name.trim() || SHADOW_AI_DEFAULT_NAME}</Text>
        </SummaryRow>
        <SummaryRow label="Decides over">
          <div className="flex flex-wrap justify-end gap-1.5">
            {["Harnesses", "Assistants", "MCP servers"].map((label) => (
              <Badge key={label} variant="neutral">
                {label}
              </Badge>
            ))}
          </div>
        </SummaryRow>
        <SummaryRow label="Action">
          <Text small>Deny the request</Text>
        </SummaryRow>
        <SummaryRow label="Unreviewed tools">
          <Text small>{dispositionLabel(disposition)}</Text>
        </SummaryRow>
        <SummaryRow label="Severity">
          <SeverityBadge score={score} />
        </SummaryRow>
        <SummaryRow label="Audience">
          <Text small>
            {audienceType === "everyone"
              ? "Everyone"
              : `${audiencePrincipalCount} selected`}
          </Text>
        </SummaryRow>
        {userMessage.trim() ? (
          <SummaryRow label="Block message">
            <Text small className="text-right">
              {userMessage}
            </Text>
          </SummaryRow>
        ) : null}
      </Stack>
    </Card>
  );
}

/**
 * Editor for the project's Shadow AI policy: the blocking Shadow MCP policy
 * re-presented as the place that decides which AI tools and MCP servers may
 * connect. Two steps on create (Access → Review); once the policy exists the
 * Access step also carries the tool decisions, since MCP decisions attach to
 * the policy's review of record and need it to exist first.
 */
export function ShadowAIPolicyEditor({
  policy,
}: {
  policy: RiskPolicy | null;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [step, setStep] = useStepParam(SHADOW_AI_STEPS);

  const [name, setName] = useState(policy?.name ?? SHADOW_AI_DEFAULT_NAME);
  const [action, setAction] = useState<PolicyAction>("block");
  const [score, setScore] = useState(policy?.score ?? SHADOW_AI_DEFAULT_SCORE);
  const [audienceType, setAudienceType] = useState<AudienceType>(() =>
    audienceTypeOf(policy),
  );
  const [audiencePrincipalUrns, setAudiencePrincipalUrns] = useState<
    Set<string>
  >(() => audiencePrincipalsOf(policy));
  const [userMessage, setUserMessage] = useState(policy?.userMessage ?? "");

  const disposition: ShadowMCPDisposition =
    policy?.shadowMcpDisposition ??
    (policy ? "block_all" : SHADOW_AI_DISPOSITION);

  const dirty =
    policy !== null &&
    (name.trim() !== policy.name ||
      score !== (policy.score ?? SHADOW_AI_DEFAULT_SCORE) ||
      audienceType !== audienceTypeOf(policy) ||
      !sameSet(audiencePrincipalUrns, audiencePrincipalsOf(policy)) ||
      userMessage !== (policy.userMessage ?? ""));

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllShadowMCPInventory(queryClient);
      toast.success("Policy updated");
    },
  });
  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: (created) => {
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllShadowMCPInventory(queryClient);
      toast.success("Shadow AI policy created. Record tool decisions below.");
      routes.policyCenter.detail.goTo(created.id);
    },
  });
  const saving = updateMutation.isPending || createMutation.isPending;

  const principals =
    audienceType === "targeted" ? [...audiencePrincipalUrns] : [];
  // The server rejects a targeted audience with nobody in it; the standard
  // editor holds the button the same way.
  const targetedWithoutPrincipals =
    audienceType === "targeted" && audiencePrincipalUrns.size === 0;
  const trimmedName = name.trim() || SHADOW_AI_DEFAULT_NAME;

  const save = () => {
    if (policy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: policy.id,
            name: trimmedName,
            sources: SHADOW_AI_SOURCES,
            action: "block",
            audienceType,
            audiencePrincipalUrns: principals,
            autoName: false,
            userMessage,
            score,
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
          sources: SHADOW_AI_SOURCES,
          action: "block",
          audienceType,
          audiencePrincipalUrns: principals,
          autoName: false,
          ...(userMessage.trim() ? { userMessage } : {}),
          score,
          shadowMcpDisposition: SHADOW_AI_DISPOSITION,
        },
      },
    });
  };

  const header = (
    <PolicyHeader
      kind="shadow_ai"
      policy={policy}
      name={name}
      onNameChange={setName}
      dirty={dirty}
      saving={saving}
      actionDisabled={targetedWithoutPrincipals}
      onSubmit={save}
      onCreate={save}
    />
  );

  return (
    <StepperShell
      header={header}
      steps={SHADOW_AI_STEPS}
      current={step}
      onStep={setStep}
    >
      {step === 0 && (
        <>
          <ActionStep
            action={action}
            setAction={setAction}
            audienceType={audienceType}
            setAudienceType={setAudienceType}
            audiencePrincipalUrns={audiencePrincipalUrns}
            setAudiencePrincipalUrns={setAudiencePrincipalUrns}
            userMessage={userMessage}
            setUserMessage={setUserMessage}
            score={score}
            setScore={setScore}
            pinnedAction={{
              value: "block",
              reason: SHADOW_AI_ACTION_LOCK_REASON,
            }}
          />
          {policy ? (
            <ToolDecisionsSection policy={policy} />
          ) : (
            <Card>
              <SectionHeader
                title="Tool decisions"
                description="Every harness, assistant and MCP server found on devices and in gateway traffic lists here once the policy exists. Tools start unreviewed; approve or disallow them one by one."
              />
            </Card>
          )}
        </>
      )}
      {step === 1 && (
        <ShadowAIReview
          name={name}
          score={score}
          audienceType={audienceType}
          audiencePrincipalCount={audiencePrincipalUrns.size}
          userMessage={userMessage}
          disposition={disposition}
        />
      )}
      <StepNav step={step} count={SHADOW_AI_STEPS.length} onStep={setStep} />
    </StepperShell>
  );
}
