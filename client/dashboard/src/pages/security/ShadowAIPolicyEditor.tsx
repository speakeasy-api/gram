import { Card } from "@/components/ui/Card";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { invalidateAllAiDetectionUsers } from "@gram/client/react-query/aiDetectionUsers.js";
import {
  invalidateAllAiDetections,
  useAiDetections,
} from "@gram/client/react-query/aiDetections.js";
import {
  invalidateAllAiScanTargets,
  useAiScanTargets,
} from "@gram/client/react-query/aiScanTargets.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { invalidateAllRiskPoliciesGet } from "@gram/client/react-query/riskPoliciesGet.js";
import { useRiskPoliciesUpdateMutation } from "@gram/client/react-query/riskPoliciesUpdate.js";
import { useSetAIToolDecisionMutation } from "@gram/client/react-query/setAIToolDecision.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { PolicyAudiencePicker } from "./PolicyCenter";
import {
  ActionMessageField,
  PolicyHeader,
  SectionHeader,
  SeveritySection,
  StepNav,
  StepperShell,
  SummaryRow,
} from "./policy-editor-shell";
import type { ShadowMCPDisposition } from "./policy-shadow-mcp-setup";
import { SeverityBadge } from "./risk-ui";
import {
  type AccessDecision,
  buildAccessRows,
  changedDecisions,
  countDecisions,
  type DecisionChange,
  type DecisionCounts,
} from "./shadow-ai/access-rows";
import { ShadowAIAccessStep } from "./shadow-ai/ShadowAIAccessStep";
import { type PolicyStep, useStepParam } from "./use-step-param";

const SHADOW_AI_STEPS: PolicyStep[] = [
  {
    id: "access",
    title: "Access",
    description:
      "Approve or deny each AI tool in the organization's target library.",
  },
  {
    id: "scope",
    title: "Scope",
    description:
      "Choose who this policy applies to, how severe its findings are, and what a refused user sees.",
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
 *  someone denies it. The disposition cannot change after create. */
const SHADOW_AI_DISPOSITION: ShadowMCPDisposition = "allow_all";

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
    ? "Allowed until denied"
    : "MCP servers blocked until allowed";
}

/** The Scope step: who the policy applies to, what a refused user sees, and
 *  how severe its findings are. The action is fixed, so it is not shown here;
 *  the Review step names it. */
function ShadowAIScopeStep({
  audienceType,
  setAudienceType,
  audiencePrincipalUrns,
  setAudiencePrincipalUrns,
  userMessage,
  setUserMessage,
  score,
  setScore,
}: {
  audienceType: AudienceType;
  setAudienceType: (value: AudienceType) => void;
  audiencePrincipalUrns: Set<string>;
  setAudiencePrincipalUrns: (value: Set<string>) => void;
  userMessage: string;
  setUserMessage: React.Dispatch<React.SetStateAction<string>>;
  score: number;
  setScore: React.Dispatch<React.SetStateAction<number>>;
}): JSX.Element {
  return (
    <div className="space-y-4">
      <Card>
        <SectionHeader description="Choose who this policy applies to and what a refused user sees." />
        <Stack gap={5}>
          <PolicyAudiencePicker
            formAudienceType={audienceType}
            setFormAudienceType={setAudienceType}
            selectedAudiencePrincipalUrns={audiencePrincipalUrns}
            setSelectedAudiencePrincipalUrns={setAudiencePrincipalUrns}
          />
          <Text small muted>
            Audience applies to MCP server enforcement. Harness and assistant
            decisions apply to everyone in the organization.
          </Text>
          <ActionMessageField
            action="block"
            userMessage={userMessage}
            setUserMessage={setUserMessage}
            label="Message shown when a server is refused"
            description="Shown to the user when a call to a denied MCP server is refused. Harness refusals at sign-in use the gateway's own message."
          />
        </Stack>
      </Card>
      <Card>
        <SeveritySection score={score} setScore={setScore} />
      </Card>
    </div>
  );
}

function ShadowAIReview({
  name,
  counts,
  score,
  audienceType,
  audiencePrincipalCount,
  userMessage,
  disposition,
}: {
  name: string;
  counts: DecisionCounts;
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
        <SummaryRow label="Decisions">
          <div className="flex flex-col items-end gap-0.5">
            <Text small>
              {counts.approved} approved · {counts.blocked} denied ·{" "}
              {counts.unreviewed} unreviewed
            </Text>
            <Text small muted>
              {counts.inventoryOnly} inventory only
            </Text>
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
 * re-presented as the place that decides which AI tools may connect. Three
 * steps, Access → Scope → Review. Access decisions are drafted over the
 * organization's whole target library and written only on Create or Save,
 * after the policy itself; MCP servers are decided on the Shadow MCP
 * inventory and are not part of the draft.
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
  const [score, setScore] = useState(policy?.score ?? SHADOW_AI_DEFAULT_SCORE);
  const [audienceType, setAudienceType] = useState<AudienceType>(() =>
    audienceTypeOf(policy),
  );
  const [audiencePrincipalUrns, setAudiencePrincipalUrns] = useState<
    Set<string>
  >(() => audiencePrincipalsOf(policy));
  const [userMessage, setUserMessage] = useState(policy?.userMessage ?? "");

  // The library with its decisions, and the detections for evidence. The
  // draft holds only what was pressed; everything else reads from the target.
  const targetsQuery = useAiScanTargets(undefined, undefined, {
    throwOnError: false,
  });
  const detectionsQuery = useAiDetections(undefined, undefined, {
    throwOnError: false,
  });
  const rows = useMemo(
    () =>
      buildAccessRows(
        targetsQuery.data?.targets ?? [],
        detectionsQuery.data?.detections ?? [],
      ),
    [targetsQuery.data?.targets, detectionsQuery.data?.detections],
  );
  const [draft, setDraft] = useState<ReadonlyMap<string, AccessDecision>>(
    () => new Map(),
  );
  const decide = (targetId: string, decision: AccessDecision) =>
    setDraft((current) => new Map(current).set(targetId, decision));
  const changes = changedDecisions(rows, draft);
  const counts = countDecisions(rows, draft);
  // What gets written is read at write time, not from the render that
  // started the save: the controls lock while saving, but a press that lands
  // between the click and the lock must still be the one applied.
  const latest = useRef({ rows, draft });
  latest.current = { rows, draft };

  const disposition: ShadowMCPDisposition =
    policy?.shadowMcpDisposition ??
    (policy ? "block_all" : SHADOW_AI_DISPOSITION);

  const dirty =
    policy !== null &&
    (name.trim() !== policy.name ||
      score !== (policy.score ?? SHADOW_AI_DEFAULT_SCORE) ||
      audienceType !== audienceTypeOf(policy) ||
      !sameSet(audiencePrincipalUrns, audiencePrincipalsOf(policy)) ||
      userMessage !== (policy.userMessage ?? "") ||
      changes.length > 0);

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllShadowMCPInventory(queryClient);
    },
  });
  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllShadowMCPInventory(queryClient);
    },
  });
  const decisionMutation = useSetAIToolDecisionMutation();
  const [applying, setApplying] = useState(false);
  const saving =
    updateMutation.isPending || createMutation.isPending || applying;

  const principals =
    audienceType === "targeted" ? [...audiencePrincipalUrns] : [];
  // The server rejects a targeted audience with nobody in it; the standard
  // editor holds the button the same way.
  const targetedWithoutPrincipals =
    audienceType === "targeted" && audiencePrincipalUrns.size === 0;
  const trimmedName = name.trim() || SHADOW_AI_DEFAULT_NAME;

  // Every changed decision, one by one, after the policy write. A failure
  // leaves the rest applied and is returned so the save can say which
  // targets are still unreviewed; the draft keeps the failed entries, so
  // Save offers them again.
  const applyDecisions = async (): Promise<DecisionChange[]> => {
    const pending = changedDecisions(latest.current.rows, latest.current.draft);
    const failed: DecisionChange[] = [];
    for (const change of pending) {
      try {
        await decisionMutation.mutateAsync({
          request: {
            setAIToolDecisionRequestBody: {
              targetId: change.targetId,
              decision: change.decision,
              rationale: change.rationale,
            },
          },
        });
      } catch {
        failed.push(change);
      }
    }
    if (pending.length > 0) {
      await Promise.all([
        invalidateAllAiScanTargets(queryClient),
        invalidateAllAiDetections(queryClient),
        invalidateAllAiDetectionUsers(queryClient),
      ]);
    }
    return failed;
  };

  const reportDecisions = (failed: DecisionChange[], written: string) => {
    if (failed.length === 0) {
      toast.success(written);
      return;
    }
    const names = failed.map((change) => change.displayName).join(", ");
    toast.error(
      `${written}, but the decision for ${names} could not be saved, so ${failed.length === 1 ? "it stays" : "they stay"} unreviewed. Open the policy and try again.`,
    );
  };

  const save = async () => {
    setApplying(true);
    try {
      if (policy) {
        await updateMutation.mutateAsync({
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
        reportDecisions(await applyDecisions(), "Policy updated");
        return;
      }
      const created = await createMutation.mutateAsync({
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
      reportDecisions(await applyDecisions(), "Shadow AI policy created");
      routes.policyCenter.detail.goTo(created.id);
    } catch {
      toast.error(
        policy
          ? "The policy could not be saved. Try again."
          : "The policy could not be created. Try again.",
      );
    } finally {
      setApplying(false);
    }
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
      onSubmit={() => void save()}
      onCreate={() => void save()}
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
        <ShadowAIAccessStep
          rows={rows}
          isPending={targetsQuery.isPending}
          libraryFailed={targetsQuery.isError}
          detectionsFailed={detectionsQuery.isError}
          draft={draft}
          disabled={saving}
          onDecide={decide}
          disposition={disposition}
        />
      )}
      {step === 1 && (
        <ShadowAIScopeStep
          audienceType={audienceType}
          setAudienceType={setAudienceType}
          audiencePrincipalUrns={audiencePrincipalUrns}
          setAudiencePrincipalUrns={setAudiencePrincipalUrns}
          userMessage={userMessage}
          setUserMessage={setUserMessage}
          score={score}
          setScore={setScore}
        />
      )}
      {step === 2 && (
        <ShadowAIReview
          name={name}
          counts={counts}
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
