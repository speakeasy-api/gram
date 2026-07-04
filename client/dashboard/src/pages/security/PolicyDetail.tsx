import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  invalidateAllRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import {
  invalidateAllRiskPoliciesGet,
  useRiskPoliciesGet,
} from "@gram/client/react-query/riskPoliciesGet.js";
import { riskEvalsEvaluate } from "@gram/client/funcs/riskEvalsEvaluate.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { Badge, Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Loader2,
  Pencil,
  Shield,
  Sparkles,
  ThumbsDown,
  ThumbsUp,
} from "lucide-react";
import {
  Fragment,
  type ReactNode,
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useParams } from "react-router";
import { useQueryState } from "nuqs";
import { type Step } from "@/pages/setup/components/onboarding-stepper";
import {
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  RULE_CATEGORY_META,
  type PolicyAction,
  type PolicyMessageType,
  type RuleCategory,
} from "./policy-data";
import {
  ActionPicker,
  CustomizeRulesSheet,
  DetectorCard,
  PolicyAudiencePicker,
  RuleSelectList,
  ScopeCard,
} from "./PolicyCenter";
import {
  ALL_CATEGORIES,
  ALL_POLICY_MESSAGE_TYPES,
  CATEGORY_LEVEL_DETECTORS,
  FLAG_ONLY_CATEGORIES,
  SCOPE_EXEMPT_CEL_EXAMPLES,
  SCOPE_INCLUDE_CEL_EXAMPLES,
  categoriesToPayload,
  pinnedHiddenRuleIds,
  policyMessageTypesForForm,
  policyMessageTypesForPayload,
  policyToCategories,
} from "./policy-form";
import { CelExpressionField } from "./cel-field";
import { useCelStatus } from "./use-cel-status";
import { useDetectionRulesStore } from "./detection-rules-data";
import { PROMPT_POLICY_TEMPLATES } from "./prompt-policy-templates";
import { SortBy, SortOrder } from "@gram/client/models/operations/listchats";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useLoadChat } from "@gram/client/react-query/loadChat.js";
import {
  invalidateAllRiskListEvalReviews,
  useRiskListEvalReviews,
} from "@gram/client/react-query/riskListEvalReviews.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useRiskSaveEvalReviewMutation } from "@gram/client/react-query/riskSaveEvalReview.js";
import { useRiskDeleteEvalReviewMutation } from "@gram/client/react-query/riskDeleteEvalReview.js";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { PromptGuardrailMessageVerdict } from "@gram/client/models/components/promptguardrailmessageverdict.js";

// Judge models offered in the workbench (mirrors PolicyCenter's list; the
// picker is intentionally small until the model catalog is centralized).
// Sentinel for the "use server default" model option — Radix Select forbids an
// empty-string item value, so "" is mapped through this and back on change.
const DEFAULT_MODEL_VALUE = "__default__";

const JUDGE_MODELS: { value: string; label: string }[] = [
  { value: "", label: "Default (Gemini 3.1 Flash Lite)" },
  { value: "google/gemini-2.5-flash", label: "Gemini 2.5 Flash" },
  { value: "anthropic/claude-sonnet-4.6", label: "Claude Sonnet 4.6" },
  { value: "anthropic/claude-haiku-4.5", label: "Claude Haiku 4.5" },
];

const PROMPT_STEPS: Step[] = [
  {
    id: "guardrail",
    title: "Guardrail",
    description: "Describe the behavior to catch and pick the judge.",
  },
  {
    id: "scope",
    title: "Scope",
    description: "Choose which messages the judge evaluates.",
  },
  {
    id: "evaluate",
    title: "Evaluate",
    description: "Preview what it catches before choosing an action.",
  },
  {
    id: "action",
    title: "Action",
    description: "Decide what happens on a match, and who it applies to.",
  },
  {
    id: "review",
    title: "Review",
    description: "Confirm the configuration before creating the policy.",
  },
];

const STANDARD_STEPS: Step[] = [
  {
    id: "detect",
    title: "Detect",
    description: "Turn on detector categories and custom rules.",
  },
  {
    id: "scope",
    title: "Scope",
    description: "Narrow where the policy applies.",
  },
  {
    id: "action",
    title: "Action",
    description: "Choose the response and audience.",
  },
  {
    id: "review",
    title: "Review",
    description: "Confirm the configuration before creating the policy.",
  },
];

// Back the active step with a `?step=<id>` URL param so browser back/forward
// (and refresh, and shareable links) traverse the steps. history: "push" makes
// each step change its own history entry.
function useStepParam(steps: Step[]): [number, (index: number) => void] {
  const [raw, setRaw] = useQueryState("step", { history: "push" });
  const found = steps.findIndex((s) => s.id === raw);
  const index = found >= 0 ? found : 0;
  const setIndex = (i: number) => {
    void setRaw(steps[i]?.id ?? null);
  };
  return [index, setIndex];
}

// Mock title summarization from the guardrail prompt (a real impl would use the
// LLM). Matches a known template name, else derives a short title.
function summarizeTitle(prompt: string): string {
  const trimmed = prompt.trim();
  const tmpl = PROMPT_POLICY_TEMPLATES.find((t) => t.prompt.trim() === trimmed);
  if (tmpl) return tmpl.name;
  const first = (trimmed.split("\n")[0] ?? "").replace(/[.!?]+$/, "").trim();
  const short = first.split(/\s+/).slice(0, 6).join(" ");
  return short ? short.charAt(0).toUpperCase() + short.slice(1) : "Untitled";
}

export default function PolicyDetail(): JSX.Element {
  const { policyId } = useParams<{ policyId: string }>();
  return (
    <RequireScope scope="org:admin" level="page">
      <PolicyDetailContent policyId={policyId!} />
    </RequireScope>
  );
}

function PolicyDetailContent({ policyId }: { policyId: string }): JSX.Element {
  const { data: policy, isLoading } = useRiskPoliciesGet({ id: policyId });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={
            policy ? { [policyId]: policy.name } : { [policyId]: "Policy" }
          }
        />
      </Page.Header>
      <Page.Body>
        {isLoading || !policy ? (
          <div className="flex items-center justify-center py-24">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        ) : policy.policyType === "prompt_based" ? (
          <PromptPolicyEditor policy={policy} />
        ) : (
          <StandardPolicyEditor policy={policy} />
        )}
      </Page.Body>
    </Page>
  );
}

// ── Create page (serves both standard and prompt policies) ───────────────────

export function PolicyNew(): JSX.Element {
  const [kind] = useQueryState("kind");
  if (kind === "standard") {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <StandardPolicyEditor policy={null} />
        </Page.Body>
      </Page>
    );
  }
  if (kind === "prompt") {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <PromptPolicyEditor policy={null} />
        </Page.Body>
      </Page>
    );
  }
  return <PolicyKindChooser />;
}

// Kind chooser shown when the create page is opened without a `?kind=` hint
// (e.g. a direct navigation). Mirrors PolicyCenter's modal chooser.
function PolicyKindChooser(): JSX.Element {
  const [, setKind] = useQueryState("kind");
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Stack gap={4} className="mx-auto w-full max-w-2xl">
          <Stack gap={1}>
            <Heading variant="h3" className="normal-case">
              Choose policy type
            </Heading>
            <Type small muted>
              Start with a detector-based policy or define criteria in plain
              language.
            </Type>
          </Stack>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <button
              type="button"
              onClick={() => void setKind("standard")}
              className="hover:bg-muted/40 rounded-xl border p-5 text-left transition-colors"
            >
              <Shield className="text-muted-foreground mb-3 h-5 w-5" />
              <Type className="font-medium">Detector-based</Type>
              <Type small muted className="mt-1">
                Scan for secrets, PII, and risky tool calls with built-in and
                custom detection rules.
              </Type>
            </button>
            <button
              type="button"
              onClick={() => void setKind("prompt")}
              className="hover:bg-muted/40 rounded-xl border p-5 text-left transition-colors"
            >
              <Sparkles className="text-muted-foreground mb-3 h-5 w-5" />
              <Type className="font-medium">Prompt-based</Type>
              <Type small muted className="mt-1">
                Describe the behavior to catch in plain language; an LLM judge
                evaluates each in-scope message.
              </Type>
            </button>
          </div>
        </Stack>
      </Page.Body>
    </Page>
  );
}

// ── Stepper shell (pinned header + left rail + spacious content column) ───────

// Horizontal stepper across the top: a left rail competes with the app's own
// sidebar, so the step nav runs horizontally and the step content gets the full
// width below it. Free navigation — any step is clickable.
function HorizontalStepper({
  steps,
  current,
  onStep,
}: {
  steps: Step[];
  current: number;
  onStep: (index: number) => void;
}): JSX.Element {
  return (
    <nav aria-label="Progress" className="flex items-center">
      {steps.map((step, index) => {
        const isDone = index < current;
        const isCurrent = index === current;
        return (
          <Fragment key={step.id}>
            <button
              type="button"
              onClick={() => onStep(index)}
              className="group flex shrink-0 items-center gap-2 rounded-md py-1 pr-1 text-left"
            >
              <span
                className={cn(
                  "flex h-7 w-7 items-center justify-center rounded-full text-sm font-semibold transition-colors",
                  isCurrent
                    ? "bg-foreground text-background"
                    : isDone
                      ? "bg-foreground/90 text-background group-hover:bg-foreground"
                      : "border-border text-muted-foreground border group-hover:border-foreground/40",
                )}
              >
                {isDone ? (
                  <Check className="h-3.5 w-3.5" strokeWidth={2.5} />
                ) : (
                  index + 1
                )}
              </span>
              <span
                className={cn(
                  "hidden text-sm font-medium sm:inline",
                  isCurrent ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {step.title}
              </span>
            </button>
            {index < steps.length - 1 && (
              <div className="bg-border mx-2 h-px min-w-6 flex-1" />
            )}
          </Fragment>
        );
      })}
    </nav>
  );
}

function StepperShell({
  header,
  steps,
  current,
  onStep,
  children,
}: {
  header: React.ReactNode;
  steps: Step[];
  current: number;
  onStep: (index: number) => void;
  children: React.ReactNode;
}): JSX.Element {
  return (
    // Full width — Page.Body already centers content at max-w-7xl (the app's
    // standard page width).
    <Stack gap={6} className="w-full">
      {header}
      <div className="bg-muted/20 rounded-lg border px-4 py-3">
        <HorizontalStepper steps={steps} current={current} onStep={onStep} />
      </div>
      <Stack gap={6}>{children}</Stack>
    </Stack>
  );
}

// Back / Continue footer for stepping through sections; the left rail also
// allows free navigation.
function StepNav({
  step,
  count,
  onStep,
}: {
  step: number;
  count: number;
  onStep: (index: number) => void;
}): JSX.Element {
  return (
    <Stack direction="horizontal" justify="space-between" gap={3}>
      {step > 0 ? (
        <Button variant="secondary" onClick={() => onStep(step - 1)}>
          <Button.Text>Back</Button.Text>
        </Button>
      ) : (
        <span />
      )}
      {step < count - 1 ? (
        <Button variant="secondary" onClick={() => onStep(step + 1)}>
          <Button.Text>Continue</Button.Text>
        </Button>
      ) : (
        <span />
      )}
    </Stack>
  );
}

// ── Shared header (pinned, both kinds) ───────────────────────────────────────

function PolicyHeader({
  kind,
  policy,
  name,
  onNameChange,
  dirty,
  saving,
  actionDisabled,
  onSubmit,
  onCreate,
  nameGenerating,
}: {
  kind: "prompt" | "standard";
  policy: RiskPolicy | null;
  name: string;
  onNameChange: (v: string) => void;
  dirty: boolean;
  saving: boolean;
  actionDisabled: boolean;
  onSubmit: () => void;
  onCreate?: () => void;
  // Shimmer the title while it's being auto-generated from the guardrail.
  nameGenerating?: boolean;
}): JSX.Element {
  const KindIcon = kind === "prompt" ? Sparkles : Shield;
  const kindLabel =
    kind === "prompt" ? "Prompt-based (LLM judge)" : "Detector-based";
  const placeholder =
    kind === "prompt" ? "Untitled prompt policy" : "Untitled standard policy";
  const isCreate = policy === null;
  const routes = useRoutes();
  const [editingName, setEditingName] = useState(false);

  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="space-between"
      gap={4}
    >
      <Stack gap={1} className="min-w-0 flex-1">
        <Stack direction="horizontal" gap={2} align="center">
          <KindIcon className="text-muted-foreground h-4 w-4 shrink-0" />
          {editingName ? (
            <Input
              value={name}
              onChange={onNameChange}
              placeholder={placeholder}
              autoFocus
              onBlur={() => setEditingName(false)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === "Escape") {
                  setEditingName(false);
                }
              }}
              className="h-9 w-[24rem] max-w-full text-lg font-semibold"
            />
          ) : (
            <button
              type="button"
              onClick={() => setEditingName(true)}
              className="group flex min-w-0 items-center gap-2"
              title="Rename policy"
            >
              <Heading
                variant="h3"
                className={cn(
                  "truncate normal-case",
                  !name && "text-muted-foreground",
                  nameGenerating && "animate-pulse",
                )}
              >
                {name || placeholder}
              </Heading>
              <Pencil className="text-muted-foreground h-4 w-4 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
            </button>
          )}
          {policy ? <StatusBadge /> : null}
        </Stack>
        {policy ? (
          <Type small muted>
            Version {policy.version} · {kindLabel}
          </Type>
        ) : (
          <Type small muted>
            {kind === "prompt"
              ? "Describe the behavior to catch. The name is generated for you."
              : "Leave the name blank to auto-generate one from the detectors."}
          </Type>
        )}
      </Stack>
      <Stack direction="horizontal" gap={2} align="center" className="shrink-0">
        <Button variant="tertiary" onClick={() => routes.policyCenter.goTo()}>
          <Button.Text>{isCreate ? "Cancel" : "Close"}</Button.Text>
        </Button>
        {isCreate ? (
          <CreateButton
            disabled={actionDisabled || saving}
            saving={saving}
            onCreate={() => onCreate?.()}
          />
        ) : (
          dirty && (
            <Button
              variant="secondary"
              disabled={actionDisabled || saving}
              onClick={onSubmit}
            >
              <Button.Text>{saving ? "Saving…" : "Save changes"}</Button.Text>
            </Button>
          )
        )}
      </Stack>
    </Stack>
  );
}

function CreateButton({
  disabled,
  saving,
  onCreate,
}: {
  disabled: boolean;
  saving: boolean;
  onCreate: () => void;
}): JSX.Element {
  return (
    <Button disabled={disabled} onClick={onCreate}>
      <Button.Text>{saving ? "Creating…" : "Create policy"}</Button.Text>
    </Button>
  );
}

function StatusBadge(): JSX.Element {
  return <Badge variant="success">Enforcing</Badge>;
}

// Vertical section header — title stacked over subtext with breathing room.
// (Card.Header lays title + description side-by-side, which reads cramped.)
// Title is optional: the step name already shows in the stepper, so primary
// step sections pass description only (avoids repeating "Guardrail"/"Scope"/…
// in both the progress rail and the card). Sub-sections (e.g. Judge) keep a
// title since they aren't named by the stepper.
function SectionHeader({
  title,
  description,
}: {
  title?: string;
  description?: string;
}): JSX.Element {
  return (
    <div className="space-y-1.5">
      {title ? (
        <Heading variant="h4" className="leading-none normal-case">
          {title}
        </Heading>
      ) : null}
      {description ? (
        <Type small muted>
          {description}
        </Type>
      ) : null}
    </div>
  );
}

// ── Prompt policy editor (stepped: Guardrail → Scope → Evaluate → Action → Review)
// One editor serves both create (policy === null) and edit.

function PromptPolicyEditor({
  policy,
}: {
  policy: RiskPolicy | null;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const isCreate = policy === null;

  const [step, setStep] = useStepParam(PROMPT_STEPS);
  const [nameGenerating, setNameGenerating] = useState(false);

  // Editable guardrail definition, seeded from the loaded policy (edit) or
  // defaults (create). Kept local so the author can iterate freely.
  const [name, setName] = useState(policy?.name ?? "");
  const [prompt, setPrompt] = useState(policy?.prompt ?? "");
  const [model, setModel] = useState(policy?.modelConfig?.model ?? "");
  const [temperature, setTemperature] = useState(
    policy?.modelConfig?.temperature ?? 0,
  );
  const [failOpen, setFailOpen] = useState(
    policy?.modelConfig?.failOpen ?? true,
  );
  const [messageTypes, setMessageTypes] = useState<Set<PolicyMessageType>>(() =>
    policyMessageTypesForForm(policy?.messageTypes),
  );
  const [scopeInclude, setScopeInclude] = useState(policy?.scopeInclude ?? "");
  const [scopeExempt, setScopeExempt] = useState(policy?.scopeExempt ?? "");
  const [scopeMode, setScopeMode] = useState<"messageTypes" | "cel">(
    policy?.scopeInclude ? "cel" : "messageTypes",
  );
  const [action, setAction] = useState<PolicyAction>(
    policy?.action === "block" ? "block" : "flag",
  );
  const [audienceType, setAudienceType] = useState<"everyone" | "targeted">(
    policy?.audienceType === "targeted" ? "targeted" : "everyone",
  );
  const [audiencePrincipalUrns, setAudiencePrincipalUrns] = useState<
    Set<string>
  >(() =>
    policy?.audienceType === "targeted"
      ? new Set(policy.audiencePrincipalUrns ?? [])
      : new Set<string>(),
  );
  const [userMessage, setUserMessage] = useState(policy?.userMessage ?? "");

  const dirty =
    !!policy &&
    (name !== policy.name ||
      prompt !== (policy.prompt ?? "") ||
      model !== (policy.modelConfig?.model ?? "") ||
      temperature !== (policy.modelConfig?.temperature ?? 0) ||
      failOpen !== (policy.modelConfig?.failOpen ?? true) ||
      scopeInclude !== (policy.scopeInclude ?? "") ||
      scopeExempt !== (policy.scopeExempt ?? "") ||
      action !== (policy.action === "block" ? "block" : "flag") ||
      userMessage !== (policy.userMessage ?? "") ||
      audienceType !==
        (policy.audienceType === "targeted" ? "targeted" : "everyone") ||
      !sameSet(
        audiencePrincipalUrns,
        new Set(
          policy.audienceType === "targeted"
            ? (policy.audiencePrincipalUrns ?? [])
            : [],
        ),
      ) ||
      !sameSet(messageTypes, policyMessageTypesForForm(policy.messageTypes)));

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskListPolicies(queryClient);
    },
  });
  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
      routes.policyCenter.goTo();
    },
  });
  const saving = updateMutation.isPending || createMutation.isPending;
  const evalReview = useEvalVerdicts(policy?.id ?? null);

  // Leaving the Guardrail step with no name yet: auto-summarize a title from
  // the guardrail (with a brief shimmer), like naming after "Continue".
  const handleStep = (next: number) => {
    if (
      step === 0 &&
      next !== 0 &&
      name.trim() === "" &&
      prompt.trim() !== ""
    ) {
      setName(summarizeTitle(prompt));
      setNameGenerating(true);
      setTimeout(() => setNameGenerating(false), 1200);
    }
    setStep(next);
  };

  // Scope is a mutex: message-type mode sends the selected parts (CEL cleared),
  // CEL mode sends the include expression (message types cleared).
  const scopePayload = () => ({
    messageTypes:
      scopeMode === "messageTypes"
        ? policyMessageTypesForPayload(messageTypes)
        : [],
    scopeInclude: scopeMode === "cel" ? scopeInclude.trim() : "",
    scopeExempt: scopeExempt.trim(),
  });

  const actionPayload = () => ({
    action,
    audienceType,
    audiencePrincipalUrns:
      audienceType === "targeted" ? [...audiencePrincipalUrns] : [],
  });

  // Blank name → the backend auto-generates one from the guardrail (mirrors
  // standard policies auto-naming from detectors).
  const autoName = name.trim() === "";

  const save = () => {
    if (!policy) return;
    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: name.trim() || policy.name,
          enabled: true,
          prompt,
          modelConfig: {
            model: model || undefined,
            temperature,
            failOpen,
          },
          ...scopePayload(),
          ...actionPayload(),
          userMessage,
          autoName,
        },
      },
    });
  };

  const create = () => {
    createMutation.mutate({
      request: {
        createRiskPolicyRequestBody: {
          policyType: "prompt_based",
          ...(autoName ? {} : { name: name.trim() }),
          enabled: true,
          prompt,
          modelConfig: { model: model || undefined, temperature, failOpen },
          ...scopePayload(),
          ...actionPayload(),
          ...(userMessage.trim() ? { userMessage } : {}),
          autoName,
        },
      },
    });
  };

  const canCreate = prompt.trim().length > 0;

  // The inline guardrail the Evaluate step replays against real sessions. Memoized
  // so its identity is stable while the author isn't editing — the eval queries
  // are keyed by it, and a debounce further bounds re-judging (see EvalTuner).
  // Only message-type scope is applied server-side (CEL scope is not replayed yet).
  const guardrail = useMemo<Guardrail>(
    () => ({
      prompt,
      model,
      temperature,
      failOpen,
      messageTypes: scopeMode === "messageTypes" ? [...messageTypes] : [],
    }),
    [prompt, model, temperature, failOpen, scopeMode, messageTypes],
  );

  const header = (
    <PolicyHeader
      kind="prompt"
      policy={policy}
      name={name}
      onNameChange={setName}
      dirty={dirty}
      saving={saving}
      actionDisabled={isCreate ? !canCreate : false}
      onSubmit={() => save()}
      onCreate={create}
      nameGenerating={nameGenerating}
    />
  );

  return (
    <StepperShell
      header={header}
      steps={PROMPT_STEPS}
      current={step}
      onStep={handleStep}
    >
      {step === 0 && (
        <>
          <GuardrailCard prompt={prompt} onPromptChange={setPrompt} />
          <JudgeSection
            model={model}
            onModelChange={setModel}
            temperature={temperature}
            onTemperatureChange={setTemperature}
            failOpen={failOpen}
            onFailOpenChange={setFailOpen}
          />
        </>
      )}

      {step === 1 && (
        <ScopeStep
          description="Which messages the judge evaluates. Narrow the scope to reduce noise and cost."
          messageTypes={messageTypes}
          setMessageTypes={setMessageTypes}
          scopeMode={scopeMode}
          setScopeMode={setScopeMode}
          scopeInclude={scopeInclude}
          setScopeInclude={setScopeInclude}
          scopeExempt={scopeExempt}
          setScopeExempt={setScopeExempt}
        />
      )}

      {step === 2 && (
        <EvalTuner
          guardrail={guardrail}
          onPromptChange={setPrompt}
          verdicts={evalReview.verdicts}
          setVerdict={evalReview.setVerdict}
        />
      )}

      {step === 3 && (
        <ActionStep
          action={action}
          setAction={setAction}
          audienceType={audienceType}
          setAudienceType={setAudienceType}
          audiencePrincipalUrns={audiencePrincipalUrns}
          setAudiencePrincipalUrns={setAudiencePrincipalUrns}
          userMessage={userMessage}
          setUserMessage={setUserMessage}
        />
      )}

      {step === 4 && (
        <PromptReview
          prompt={prompt}
          model={model}
          temperature={temperature}
          failOpen={failOpen}
          scopeMode={scopeMode}
          messageTypes={messageTypes}
          scopeInclude={scopeInclude}
          scopeExempt={scopeExempt}
          action={action}
          audienceType={audienceType}
          audiencePrincipalCount={audiencePrincipalUrns.size}
          verdicts={evalReview.verdicts}
        />
      )}

      <StepNav step={step} count={PROMPT_STEPS.length} onStep={handleStep} />
    </StepperShell>
  );
}

// ── Guardrail hero (the prompt — the thing you iterate) ──────────────────────

function GuardrailCard({
  prompt,
  onPromptChange,
  rows = 12,
}: {
  prompt: string;
  onPromptChange: (v: string) => void;
  rows?: number;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description="Plain-language behavior the judge flags on each in-scope message." />
      <Stack gap={3}>
        <div className="flex flex-wrap gap-1.5">
          {PROMPT_POLICY_TEMPLATES.map((t) => (
            <button
              key={t.name}
              type="button"
              onClick={() => onPromptChange(t.prompt)}
              className="hover:bg-muted rounded-full border px-2.5 py-1 text-xs transition-colors"
            >
              {t.name}
            </button>
          ))}
        </div>
        <TextArea
          value={prompt}
          onChange={onPromptChange}
          rows={rows}
          placeholder="e.g. Flag any tool call that issues a refund without a prior authorization step."
        />
      </Stack>
    </Card>
  );
}

// ── Judge section (model · temperature · fail behavior) ──────────────────────

function JudgeSection({
  model,
  onModelChange,
  temperature,
  onTemperatureChange,
  failOpen,
  onFailOpenChange,
}: {
  model: string;
  onModelChange: (v: string) => void;
  temperature: number;
  onTemperatureChange: (v: number) => void;
  failOpen: boolean;
  onFailOpenChange: (v: boolean) => void;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader
        title="Judge"
        description="The model that evaluates each in-scope message and how it behaves under error."
      />
      <Stack gap={8}>
        {/* Model */}
        <div className="space-y-2">
          <Type small>Model</Type>
          <Type small muted>
            The LLM that judges each in-scope message.
          </Type>
          <Select
            value={model || DEFAULT_MODEL_VALUE}
            onValueChange={(v) =>
              onModelChange(v === DEFAULT_MODEL_VALUE ? "" : v)
            }
          >
            <SelectTrigger className="w-[16rem]">
              <SelectValue placeholder="Default" />
            </SelectTrigger>
            <SelectContent>
              {JUDGE_MODELS.map((m) => (
                <SelectItem
                  key={m.value || DEFAULT_MODEL_VALUE}
                  value={m.value || DEFAULT_MODEL_VALUE}
                >
                  {m.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Temperature */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Type small>Temperature</Type>
            <Type small mono>
              {temperature.toFixed(1)}
            </Type>
          </div>
          <Type small muted>
            Lower is more deterministic and repeatable; higher allows more
            nuanced judgment but less consistent results.
          </Type>
          <div className="pt-3">
            <Slider
              value={temperature}
              onChange={(v) => onTemperatureChange(Math.max(0, Math.min(1, v)))}
              min={0}
              max={1}
              step={0.1}
              ticks={[0, 0.5, 1]}
            />
          </div>
        </div>

        {/* On judge error */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Type small>On judge error</Type>
            <Stack direction="horizontal" gap={2} align="center">
              <Type small muted>
                {failOpen ? "Fail open" : "Fail closed"}
              </Type>
              <Switch checked={failOpen} onCheckedChange={onFailOpenChange} />
            </Stack>
          </div>
          <Type small muted>
            If the judge times out or errors, fail open lets the message through
            (no false blocks); fail closed blocks it (stricter, but can
            interrupt legitimate traffic).
          </Type>
        </div>
      </Stack>
    </Card>
  );
}

// ── Scope section (message types · include/exempt CEL) ───────────────────────

// Shared Scope step — used identically by prompt and standard editors:
// message-types vs CEL segmented toggle, message-type cards, and an exemptions
// allowlist. Message-type set is kept as Set<string> so both editors' state
// shapes plug in.
function ScopeStep({
  description,
  messageTypes,
  setMessageTypes,
  scopeMode,
  setScopeMode,
  scopeInclude,
  setScopeInclude,
  scopeExempt,
  setScopeExempt,
}: {
  description: string;
  messageTypes: Set<PolicyMessageType>;
  setMessageTypes: (next: Set<PolicyMessageType>) => void;
  scopeMode: "messageTypes" | "cel";
  setScopeMode: (m: "messageTypes" | "cel") => void;
  scopeInclude: string;
  setScopeInclude: (v: string) => void;
  scopeExempt: string;
  setScopeExempt: (v: string) => void;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description={description} />
      <Stack gap={5}>
        <div className="space-y-3">
          <div className="border-border inline-flex rounded-md border p-0.5">
            {(
              [
                { key: "messageTypes", label: "Message types" },
                { key: "cel", label: "CEL expression" },
              ] as const
            ).map((opt) => (
              <button
                key={opt.key}
                type="button"
                onClick={() => setScopeMode(opt.key)}
                className={cn(
                  "rounded px-3 py-1 text-xs font-medium transition-colors",
                  scopeMode === opt.key
                    ? "bg-foreground text-background"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
          <p className="text-muted-foreground text-xs">
            {scopeMode === "messageTypes"
              ? "Apply to whole session parts. Switch to a CEL expression to match on tool or content attributes instead."
              : "Apply only to messages matching the expression below — this replaces the message-type selection."}
          </p>
        </div>

        {scopeMode === "messageTypes" ? (
          <>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {ALL_POLICY_MESSAGE_TYPES.map((type) => (
                <ScopeCard
                  key={type}
                  type={type}
                  checked={messageTypes.has(type)}
                  onToggle={(checked) => {
                    const updated = new Set(messageTypes);
                    if (checked) updated.add(type);
                    else updated.delete(type);
                    setMessageTypes(updated);
                  }}
                />
              ))}
            </div>
            {messageTypes.size === 0 && (
              <p className="text-destructive text-xs">
                Select at least one session part.
              </p>
            )}
          </>
        ) : (
          <div className="space-y-2">
            <Label className="text-sm font-medium">
              Evaluate messages matching
            </Label>
            <p className="text-muted-foreground text-xs">
              The policy evaluates a message only when this expression is true.
            </p>
            <CelExpressionField
              value={scopeInclude}
              onChange={setScopeInclude}
              examples={SCOPE_INCLUDE_CEL_EXAMPLES}
            />
          </div>
        )}

        <div className="border-border space-y-4 border-t pt-6">
          <div>
            <Label className="text-sm font-medium">Exemptions</Label>
            <p className="text-muted-foreground text-xs">
              Skip the whole policy for any message matching this expression —
              an allowlist, regardless of the scope above.
            </p>
          </div>
          <CelExpressionField
            value={scopeExempt}
            onChange={setScopeExempt}
            examples={SCOPE_EXEMPT_CEL_EXAMPLES}
          />
        </div>
      </Stack>
    </Card>
  );
}

// ── Action section (flag vs block) ───────────────────────────────────────────

// Shared Action step — identical for prompt and standard: flag/block picker,
// audience, and the block-time custom message.
function ActionStep({
  action,
  setAction,
  audienceType,
  setAudienceType,
  audiencePrincipalUrns,
  setAudiencePrincipalUrns,
  userMessage,
  setUserMessage,
  flagOnlySelected = false,
}: {
  action: PolicyAction;
  setAction: React.Dispatch<React.SetStateAction<PolicyAction>>;
  audienceType: "everyone" | "targeted";
  setAudienceType: React.Dispatch<
    React.SetStateAction<"everyone" | "targeted">
  >;
  audiencePrincipalUrns: Set<string>;
  setAudiencePrincipalUrns: React.Dispatch<React.SetStateAction<Set<string>>>;
  userMessage: string;
  setUserMessage: React.Dispatch<React.SetStateAction<string>>;
  flagOnlySelected?: boolean;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description="Choose how the policy responds when it fires, and who it applies to." />
      <Stack gap={5}>
        <ActionPicker
          formAction={action}
          setFormAction={setAction}
          flagOnlySelected={flagOnlySelected}
        />
        <PolicyAudiencePicker
          formAudienceType={audienceType}
          setFormAudienceType={setAudienceType}
          selectedAudiencePrincipalUrns={audiencePrincipalUrns}
          setSelectedAudiencePrincipalUrns={setAudiencePrincipalUrns}
        />
        {action === "block" && (
          <div className="space-y-2">
            <Label className="text-sm font-medium">Custom Message</Label>
            <p className="text-muted-foreground text-xs">
              Shown to the user when this policy blocks a tool call or prompt.
              Leave blank to use the default message.
            </p>
            <TextArea
              value={userMessage}
              onChange={setUserMessage}
              placeholder="e.g. This action was blocked by your organization's security policy. Contact your admin for help."
              rows={3}
            />
          </div>
        )}
      </Stack>
    </Card>
  );
}

// ── Evaluate step: prompt-tuning workbench (session-local mock) ───────────────
// A manual tuning loop rather than a bulk run: tweak the guardrail on the left,
// watch how it judges a real session's transcript on the right, mark it, move
// on. The mock's verdicts are predefined, but the UX presents them as re-judged
// live as the prompt changes.

type EvalVerdict = "correct" | "false_positive" | "missed";
type EvalMatchFilter = "all" | "flagged" | "clean";
type JudgeAgreement = "agree" | "disagree";

// The inline guardrail replayed against real chat sessions. Mirrors the Evaluate
// step's editable state; the eval queries are keyed by it.
type Guardrail = {
  prompt: string;
  model: string;
  temperature: number;
  failOpen: boolean;
  messageTypes: string[];
};

// Cap the session picker so the lazy per-row judge (one call per visible row,
// cached per guardrail+chat) stays bounded.
const EVAL_SESSION_LIMIT = 8;

const ROLE_LABEL: Record<string, string> = {
  user: "User",
  assistant: "Assistant",
  tool: "Tool result",
};

// Build the judge request body for one chat under the current guardrail. The
// query key derives from this, so equal guardrails hit the same cache.
function evalRequestBody(guardrail: Guardrail, chatId: string) {
  return {
    evaluatePromptGuardrailRequestBody: {
      chatId,
      prompt: guardrail.prompt,
      modelConfig: {
        model: guardrail.model || undefined,
        temperature: guardrail.temperature,
        failOpen: guardrail.failOpen,
      },
      messageTypes: guardrail.messageTypes.length
        ? guardrail.messageTypes
        : undefined,
    },
  };
}

function usePromptGuardrailEval(
  guardrail: Guardrail,
  chatId: string,
  enabled: boolean,
) {
  const client = useGramContext();
  const request = useMemo(
    () => evalRequestBody(guardrail, chatId),
    [guardrail, chatId],
  );

  // The generated query hook does not include the POST body in its cache key.
  // This replay is keyed by the full inline guardrail so prompt edits and chats
  // never share stale judge results.
  return useQuery({
    queryKey: ["@gram/client", "evals", "evaluate", request],
    queryFn: ({ signal }) =>
      unwrapAsync(riskEvalsEvaluate(client, request, undefined, { signal })),
    enabled,
    ...JUDGE_QUERY_OPTIONS,
  });
}

function guardrailEvalKey(guardrail: Guardrail): string {
  return JSON.stringify({
    prompt: guardrail.prompt,
    model: guardrail.model || "",
    temperature: guardrail.temperature,
    failOpen: guardrail.failOpen,
    messageTypes: guardrail.messageTypes,
  });
}

const JUDGE_QUERY_OPTIONS = {
  // Judging costs an LLM call; once a (guardrail, chat) is judged, keep it.
  staleTime: Infinity,
  gcTime: 5 * 60 * 1000,
  refetchOnWindowFocus: false,
} as const;

// Debounce a value so live guardrail edits don't fire a judge call per keystroke.
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

// Verdicts are the review set (ground truth). On an existing policy they persist
// via the review endpoints; during create (no policy id yet) they stay
// session-local and are not saved.
function useEvalVerdicts(policyId: string | null): {
  verdicts: Map<string, EvalVerdict>;
  setVerdict: (chatId: string, verdict: EvalVerdict) => void;
} {
  const queryClient = useQueryClient();
  const [localVerdicts, setLocalVerdicts] = useState<Map<string, EvalVerdict>>(
    new Map(),
  );
  const listQuery = useRiskListEvalReviews(
    { policyId: policyId ?? "" },
    undefined,
    {
      enabled: !!policyId,
    },
  );
  const saveMutation = useRiskSaveEvalReviewMutation();
  const deleteMutation = useRiskDeleteEvalReviewMutation();

  const persistedVerdicts = useMemo(() => {
    const m = new Map<string, EvalVerdict>();
    for (const r of listQuery.data?.reviews ?? []) {
      m.set(r.chatId, r.verdict as EvalVerdict);
    }
    return m;
  }, [listQuery.data]);

  const verdicts = policyId ? persistedVerdicts : localVerdicts;

  const setVerdict = (chatId: string, verdict: EvalVerdict) => {
    const clearing = verdicts.get(chatId) === verdict; // clicking the active mark toggles it off
    if (!policyId) {
      setLocalVerdicts((prev) => {
        const next = new Map(prev);
        if (clearing) next.delete(chatId);
        else next.set(chatId, verdict);
        return next;
      });
      return;
    }
    const onSettled = () => void invalidateAllRiskListEvalReviews(queryClient);
    if (clearing) {
      deleteMutation.mutate({ request: { policyId, chatId } }, { onSettled });
    } else {
      saveMutation.mutate(
        {
          request: {
            saveRiskEvalReviewRequestBody: { policyId, chatId, verdict },
          },
        },
        { onSettled },
      );
    }
  };

  return { verdicts, setVerdict };
}

function EvalTuner({
  guardrail,
  onPromptChange,
  verdicts,
  setVerdict,
}: {
  guardrail: Guardrail;
  onPromptChange: (v: string) => void;
  verdicts: Map<string, EvalVerdict>;
  setVerdict: (chatId: string, verdict: EvalVerdict) => void;
}): JSX.Element {
  // Judge against a debounced guardrail so typing doesn't re-judge every row on
  // each keystroke; the card itself edits the prompt live.
  const judgeGuardrail = useDebounced(guardrail, 600);
  const guardrailKey = useMemo(() => guardrailEvalKey(guardrail), [guardrail]);
  const judgeGuardrailKey = useMemo(
    () => guardrailEvalKey(judgeGuardrail),
    [judgeGuardrail],
  );

  return (
    <div className="grid gap-6 @3xl:grid-cols-2">
      <Stack gap={4}>
        <GuardrailCard
          prompt={guardrail.prompt}
          onPromptChange={onPromptChange}
          rows={10}
        />
        <ReviewScorecard verdicts={verdicts} />
      </Stack>
      <SessionReview
        guardrail={judgeGuardrail}
        debouncePending={guardrailKey !== judgeGuardrailKey}
        verdicts={verdicts}
        onVerdict={setVerdict}
      />
    </div>
  );
}

function PromptReview({
  prompt,
  model,
  temperature,
  failOpen,
  scopeMode,
  messageTypes,
  scopeInclude,
  scopeExempt,
  action,
  audienceType,
  audiencePrincipalCount,
  verdicts,
}: {
  prompt: string;
  model: string;
  temperature: number;
  failOpen: boolean;
  scopeMode: "messageTypes" | "cel";
  messageTypes: Set<PolicyMessageType>;
  scopeInclude: string;
  scopeExempt: string;
  action: PolicyAction;
  audienceType: "everyone" | "targeted";
  audiencePrincipalCount: number;
  verdicts: Map<string, EvalVerdict>;
}): JSX.Element {
  const scopeText =
    scopeMode === "cel"
      ? scopeInclude.trim() || "All messages matching a CEL expression"
      : [...messageTypes]
          .map((t) => POLICY_MESSAGE_TYPE_META[t].label)
          .join(", ") || "No message types selected";
  const modelLabel =
    JUDGE_MODELS.find((m) => m.value === model)?.label ?? model;

  return (
    <Stack gap={4}>
      <Card>
        <SectionHeader description="Confirm the guardrail, scope, and response before creating the policy." />
        <Stack gap={4}>
          <SummaryRow label="Guardrail">
            <Type small className="max-w-xl text-right">
              {prompt.trim() || "No guardrail prompt configured"}
            </Type>
          </SummaryRow>
          <SummaryRow label="Judge">
            <Type small className="text-right">
              {modelLabel} · temperature {temperature.toFixed(1)} ·{" "}
              {failOpen ? "fail open" : "fail closed"}
            </Type>
          </SummaryRow>
          <SummaryRow label="Scope">
            <Type small className="text-right">
              {scopeText}
            </Type>
          </SummaryRow>
          {scopeExempt.trim() ? (
            <SummaryRow label="Exemptions">
              <Type small mono className="text-right break-all">
                {scopeExempt.trim()}
              </Type>
            </SummaryRow>
          ) : null}
          <SummaryRow label="Action">
            <Badge variant={action === "block" ? "warning" : "neutral"}>
              {action === "block" ? "Block" : "Flag"}
            </Badge>
          </SummaryRow>
          <SummaryRow label="Audience">
            <Type small>
              {audienceType === "targeted"
                ? `${audiencePrincipalCount} targeted principal${
                    audiencePrincipalCount === 1 ? "" : "s"
                  }`
                : "Everyone"}
            </Type>
          </SummaryRow>
        </Stack>
      </Card>
      <ReviewScorecard verdicts={verdicts} />
    </Stack>
  );
}

// Scorecard over the labeled review set: agreement + the two disagreement
// directions (which tell you which way to tune the guardrail).
function ReviewScorecard({
  verdicts,
}: {
  verdicts: Map<string, EvalVerdict>;
}): JSX.Element {
  const reviewed = verdicts.size;
  let correct = 0;
  let falsePositive = 0;
  let missed = 0;
  for (const v of verdicts.values()) {
    if (v === "correct") correct += 1;
    else if (v === "false_positive") falsePositive += 1;
    else if (v === "missed") missed += 1;
  }

  return (
    <Card>
      <SectionHeader description="Your review set: how well the guardrail matches your judgment on the sessions you've checked. Edits replay automatically after a short pause." />
      {reviewed === 0 ? (
        <Type small muted>
          Review a few sessions on the right to build a scorecard. Aim for a
          couple of clear matches and a clean one or two.
        </Type>
      ) : (
        <Stack gap={4}>
          <div>
            <Type className="text-2xl font-semibold">
              {correct}/{reviewed}
            </Type>
            <Type small muted>
              match your judgment
            </Type>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <ScoreStat label="Correct" value={correct} />
            <ScoreStat
              label="False positives"
              value={falsePositive}
              hint="tighten"
              warn={falsePositive > 0}
            />
            <ScoreStat
              label="Missed"
              value={missed}
              hint="broaden"
              warn={missed > 0}
            />
          </div>
        </Stack>
      )}
    </Card>
  );
}

function ScoreStat({
  label,
  value,
  hint,
  warn,
}: {
  label: string;
  value: number;
  hint?: string;
  warn?: boolean;
}): JSX.Element {
  return (
    <div className="rounded-lg border p-3">
      <Type className={cn("text-xl font-semibold", warn && "text-warning")}>
        {value}
      </Type>
      <Type small muted>
        {label}
      </Type>
      {hint && value > 0 ? (
        <Type small muted className="mt-0.5 italic">
          {hint}
        </Type>
      ) : null}
    </div>
  );
}

type JudgedMatch = {
  guardrailKey: string;
  flagged: boolean;
};

function verdictForAgreement(
  flagged: boolean,
  agreement: JudgeAgreement,
): EvalVerdict {
  if (agreement === "agree") return "correct";
  return flagged ? "false_positive" : "missed";
}

function agreementForVerdict(
  flagged: boolean,
  verdict: EvalVerdict,
): JudgeAgreement | null {
  if (verdict === "correct") return "agree";
  if (flagged && verdict === "false_positive") return "disagree";
  if (!flagged && verdict === "missed") return "disagree";
  return null;
}

function evalVerdictLabel(verdict: EvalVerdict): string {
  switch (verdict) {
    case "correct":
      return "Correct";
    case "false_positive":
      return "False positive";
    case "missed":
      return "Missed";
  }
}

function judgeSessionLabel(flagged: boolean | undefined): string {
  if (flagged === undefined) return "Judge result pending";
  return flagged
    ? "Judge flagged this session"
    : "Judge found this session clean";
}

function agreementButtonClass(
  agreement: JudgeAgreement,
  selected: boolean,
): string | undefined {
  if (!selected) return undefined;
  return agreement === "agree"
    ? "!border-success !bg-success !text-success-foreground hover:!bg-success/90"
    : "!border-destructive/40 !bg-destructive/15 !text-destructive hover:!bg-destructive/20";
}

function rowHiddenByFilter(
  filter: EvalMatchFilter,
  flagged: boolean | undefined,
): boolean {
  if (filter === "all" || flagged === undefined) return false;
  return filter === "flagged" ? !flagged : flagged;
}

function sessionUserLabel(chat: ChatOverview): string {
  return chat.externalUserId || chat.userId || "Unknown user";
}

function textContainsQuery(text: string, query: string): boolean {
  return text.toLowerCase().includes(query.toLowerCase());
}

function highlightQuery(text: string, query: string): ReactNode {
  if (query.length === 0) return text;

  const matchIndex = text.toLowerCase().indexOf(query.toLowerCase());
  if (matchIndex === -1) return text;

  return (
    <>
      {text.slice(0, matchIndex)}
      <mark className="rounded-sm bg-yellow-200 px-0.5 text-foreground dark:bg-yellow-700/60">
        {text.slice(matchIndex, matchIndex + query.length)}
      </mark>
      {text.slice(matchIndex + query.length)}
    </>
  );
}

function chatMatchesVisibleSearch(chat: ChatOverview, query: string): boolean {
  if (query.length === 0) return true;

  const title = chat.title || "Untitled session";
  const userLabel = sessionUserLabel(chat);
  return textContainsQuery(title, query) || textContainsQuery(userLabel, query);
}

function SessionReview({
  guardrail,
  debouncePending,
  verdicts,
  onVerdict,
}: {
  guardrail: Guardrail;
  debouncePending: boolean;
  verdicts: Map<string, EvalVerdict>;
  onVerdict: (chatId: string, verdict: EvalVerdict) => void;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<EvalMatchFilter>("all");
  const [selectedIdState, setSelectedIdState] = useState<string | null>(null);
  const deferredQuery = useDeferredValue(query);

  const chatsQuery = useListChats({
    search: deferredQuery.trim() || undefined,
    limit: EVAL_SESSION_LIMIT,
    sortBy: SortBy.LastMessageTimestamp,
    sortOrder: SortOrder.Desc,
  });
  const activeQuery = deferredQuery.trim();
  const guardrailKey = useMemo(() => guardrailEvalKey(guardrail), [guardrail]);
  const rawChats = chatsQuery.data?.chats;
  const chats = useMemo(
    () =>
      (rawChats ?? []).filter((chat) =>
        chatMatchesVisibleSearch(chat, activeQuery),
      ),
    [rawChats, activeQuery],
  );

  // Each row judges itself and reports its flagged state up so the match filter
  // can hide non-matching rows without unmounting them (keeping the cache warm).
  const [judged, setJudged] = useState<Map<string, JudgedMatch>>(new Map());
  const [judgingRows, setJudgingRows] = useState<Map<string, string>>(
    new Map(),
  );
  const [judgingTranscript, setJudgingTranscript] = useState<{
    chatId: string;
    guardrailKey: string;
  } | null>(null);
  const reportJudged = useCallback(
    (chatId: string, flagged: boolean, reportedGuardrailKey: string) => {
      setJudged((prev) => {
        const current = prev.get(chatId);
        if (
          current?.guardrailKey === reportedGuardrailKey &&
          current.flagged === flagged
        ) {
          return prev;
        }
        const next = new Map(prev);
        next.set(chatId, { guardrailKey: reportedGuardrailKey, flagged });
        return next;
      });
    },
    [],
  );
  const reportRowJudging = useCallback(
    (chatId: string, judging: boolean, reportedGuardrailKey: string) => {
      setJudgingRows((prev) => {
        const current = prev.get(chatId);
        if (judging) {
          if (current === reportedGuardrailKey) return prev;
          const next = new Map(prev);
          next.set(chatId, reportedGuardrailKey);
          return next;
        }
        if (current !== reportedGuardrailKey) return prev;
        const next = new Map(prev);
        next.delete(chatId);
        return next;
      });
    },
    [],
  );
  const reportTranscriptJudging = useCallback(
    (chatId: string, judging: boolean, reportedGuardrailKey: string) => {
      setJudgingTranscript((current) => {
        if (judging) return { chatId, guardrailKey: reportedGuardrailKey };
        if (
          current?.chatId !== chatId ||
          current.guardrailKey !== reportedGuardrailKey
        ) {
          return current;
        }
        return null;
      });
    },
    [],
  );

  // Derive the selected id during render so it stays valid as results change.
  const selectedId =
    selectedIdState && chats.some((c) => c.id === selectedIdState)
      ? selectedIdState
      : (chats[0]?.id ?? null);
  const selectedChat = chats.find((c) => c.id === selectedId) ?? null;
  const hasGuardrail = guardrail.prompt.trim().length > 0;
  const flaggedForCurrentGuardrail = (chatId: string) => {
    const match = judged.get(chatId);
    return match?.guardrailKey === guardrailKey ? match.flagged : undefined;
  };
  const rowIsVisible = (chat: ChatOverview) =>
    !rowHiddenByFilter(filter, flaggedForCurrentGuardrail(chat.id));
  const visibleRowsJudging = chats.some(
    (chat) => rowIsVisible(chat) && judgingRows.get(chat.id) === guardrailKey,
  );
  const transcriptJudging =
    !!selectedId &&
    judgingTranscript?.chatId === selectedId &&
    judgingTranscript.guardrailKey === guardrailKey;
  const reevaluating =
    hasGuardrail &&
    (debouncePending || visibleRowsJudging || transcriptJudging);

  return (
    <Card className="flex flex-col">
      <SectionHeader description="Search by title or user, review how this guardrail judges the transcript, then mark the verdict." />
      <div className="flex flex-1 flex-col gap-4">
        {/* Search + match filter */}
        <Stack gap={2}>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={query}
              onChange={setQuery}
              placeholder="Search by title or user"
              className="min-w-48 flex-1"
            />
            <ReevaluatingIndicator show={reevaluating} />
          </div>
          <div className="border-border inline-flex self-start rounded-md border p-0.5">
            {(
              [
                { key: "all", label: "All" },
                { key: "flagged", label: "Flagged" },
                { key: "clean", label: "Clean" },
              ] as const
            ).map((opt) => (
              <button
                key={opt.key}
                type="button"
                onClick={() => setFilter(opt.key)}
                className={cn(
                  "rounded px-3 py-1 text-xs font-medium transition-colors",
                  filter === opt.key
                    ? "bg-foreground text-background"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </Stack>

        {/* Results list */}
        <div className="max-h-56 overflow-auto rounded-lg border">
          {chatsQuery.isLoading ? (
            <Type small muted className="block px-3 py-6 text-center">
              Loading sessions…
            </Type>
          ) : chats.length === 0 ? (
            <Type small muted className="block px-3 py-6 text-center">
              No sessions match your search.
            </Type>
          ) : (
            chats.map((chat, i) => (
              <SessionRow
                key={chat.id}
                chat={chat}
                guardrail={guardrail}
                guardrailKey={guardrailKey}
                active={chat.id === selectedId}
                first={i === 0}
                enabled={hasGuardrail}
                hidden={!rowIsVisible(chat)}
                searchQuery={activeQuery}
                onSelect={() => setSelectedIdState(chat.id)}
                onJudged={reportJudged}
                onJudging={reportRowJudging}
              />
            ))
          )}
        </div>

        {/* Transcript + review controls */}
        {selectedChat && (
          <SessionTranscript
            chat={selectedChat}
            guardrail={guardrail}
            guardrailKey={guardrailKey}
            verdict={verdicts.get(selectedChat.id) ?? null}
            reviewDisabled={debouncePending || transcriptJudging}
            onVerdict={(v) => onVerdict(selectedChat.id, v)}
            onJudging={reportTranscriptJudging}
          />
        )}
      </div>
    </Card>
  );
}

function ReevaluatingIndicator({
  show,
}: {
  show: boolean;
}): JSX.Element | null {
  if (!show) return null;

  return (
    <div className="border-border bg-muted/30 text-muted-foreground flex h-9 items-center gap-1.5 rounded-md border px-2.5">
      <Loader2 className="h-3.5 w-3.5 animate-spin" />
      <Type small muted>
        Re-evaluating…
      </Type>
    </div>
  );
}

function SessionRow({
  chat,
  guardrail,
  guardrailKey,
  active,
  first,
  enabled,
  hidden,
  searchQuery,
  onSelect,
  onJudged,
  onJudging,
}: {
  chat: ChatOverview;
  guardrail: Guardrail;
  guardrailKey: string;
  active: boolean;
  first: boolean;
  enabled: boolean;
  hidden: boolean;
  searchQuery: string;
  onSelect: () => void;
  onJudged: (chatId: string, flagged: boolean, guardrailKey: string) => void;
  onJudging: (chatId: string, judging: boolean, guardrailKey: string) => void;
}): JSX.Element {
  const judge = usePromptGuardrailEval(guardrail, chat.id, enabled);
  const judging = enabled && (judge.isPending || judge.isFetching);
  const flagged = judging ? undefined : judge.data?.flagged;
  const title = chat.title || "Untitled session";
  const userLabel = sessionUserLabel(chat);

  useEffect(() => {
    if (flagged !== undefined) onJudged(chat.id, flagged, guardrailKey);
  }, [flagged, chat.id, guardrailKey, onJudged]);
  useEffect(() => {
    onJudging(chat.id, judging, guardrailKey);
  }, [chat.id, guardrailKey, judging, onJudging]);
  useEffect(
    () => () => onJudging(chat.id, false, guardrailKey),
    [chat.id, guardrailKey, onJudging],
  );

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors",
        !first && "border-t",
        active ? "bg-muted/60" : "hover:bg-muted/30",
        hidden && "hidden",
      )}
    >
      <div className="min-w-0 flex-1">
        <Type small className="truncate font-medium">
          {highlightQuery(title, searchQuery)}
        </Type>
        <Type small muted className="flex min-w-0 items-center gap-1 truncate">
          <span className="truncate">
            {highlightQuery(userLabel, searchQuery)}
          </span>
          <span>·</span>
          <span>{chat.numMessages} messages</span>
          {chat.source ? ` · ${chat.source}` : ""} ·{" "}
          {formatRelative(chat.lastMessageTimestamp)}
        </Type>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        <SessionMatchBadge
          enabled={enabled}
          judging={judging}
          flagged={flagged}
        />
      </div>
    </button>
  );
}

function SessionMatchBadge({
  enabled,
  judging,
  flagged,
}: {
  enabled: boolean;
  judging: boolean;
  flagged: boolean | undefined;
}): JSX.Element | null {
  if (!enabled) return null;
  if (flagged === undefined) {
    return (
      <Badge variant="neutral">{judging ? "Judging…" : "Not judged"}</Badge>
    );
  }
  return (
    <Badge variant={flagged ? "destructive" : "neutral"}>
      {flagged ? "Flagged" : "Clean"}
    </Badge>
  );
}

function SessionTranscript({
  chat,
  guardrail,
  guardrailKey,
  verdict,
  reviewDisabled,
  onVerdict,
  onJudging,
}: {
  chat: ChatOverview;
  guardrail: Guardrail;
  guardrailKey: string;
  verdict: EvalVerdict | null;
  reviewDisabled: boolean;
  onVerdict: (v: EvalVerdict) => void;
  onJudging: (chatId: string, judging: boolean, guardrailKey: string) => void;
}): JSX.Element {
  const chatQuery = useLoadChat({ id: chat.id, limit: 200 });
  const judge = usePromptGuardrailEval(
    guardrail,
    chat.id,
    guardrail.prompt.trim().length > 0,
  );

  const verdictByMessage = useMemo(() => {
    const m = new Map<string, PromptGuardrailMessageVerdict>();
    for (const v of judge.data?.verdicts ?? []) m.set(v.messageId, v);
    return m;
  }, [judge.data]);

  const messages = chatQuery.data?.messages ?? [];
  const judgeFlagged = judge.data?.flagged;
  const judgeSettled = judgeFlagged !== undefined && !judge.isFetching;
  const canReview = judgeSettled && !reviewDisabled;

  useEffect(() => {
    onJudging(chat.id, judge.isFetching, guardrailKey);
  }, [chat.id, guardrailKey, judge.isFetching, onJudging]);
  useEffect(
    () => () => onJudging(chat.id, false, guardrailKey),
    [chat.id, guardrailKey, onJudging],
  );

  return (
    <Stack gap={3} className="min-w-0">
      <Stack direction="horizontal" gap={2} align="center">
        <Type small className="font-medium">
          {chat.title || "Untitled session"}
        </Type>
        {judge.isFetching ? (
          <Type small muted>
            Judging…
          </Type>
        ) : null}
      </Stack>

      {chatQuery.isLoading ? (
        <Type small muted>
          Loading transcript…
        </Type>
      ) : messages.length === 0 ? (
        <Type small muted>
          This session has no messages.
        </Type>
      ) : (
        <Stack gap={2}>
          {messages.map((m) => (
            <TranscriptMessage
              key={m.id}
              message={m}
              verdict={verdictByMessage.get(m.id) ?? null}
            />
          ))}
        </Stack>
      )}

      <ReviewAgreementControl
        flagged={judgeFlagged}
        verdict={verdict}
        disabled={!canReview}
        onVerdict={onVerdict}
      />
    </Stack>
  );
}

function ReviewAgreementControl({
  flagged,
  verdict,
  disabled,
  onVerdict,
}: {
  flagged: boolean | undefined;
  verdict: EvalVerdict | null;
  disabled: boolean;
  onVerdict: (v: EvalVerdict) => void;
}): JSX.Element {
  const currentAgreement =
    flagged === undefined || verdict === null
      ? null
      : agreementForVerdict(flagged, verdict);
  const staleReview =
    verdict !== null && flagged !== undefined && !currentAgreement;
  const judgeLabel = judgeSessionLabel(flagged);
  const reviewPrompt =
    flagged === undefined
      ? "Review available after judging completes."
      : "Was the judge right?";

  const handleAgreement = (agreement: JudgeAgreement) => {
    if (flagged === undefined) return;
    onVerdict(verdictForAgreement(flagged, agreement));
  };

  return (
    <Stack gap={3} align="center" className="border-border border-t pt-3">
      <Stack gap={1} align="center" className="text-center">
        <Type small muted className="font-medium">
          {judgeLabel}
        </Type>
        <Type small muted>
          {reviewPrompt}
        </Type>
        {staleReview ? (
          <Type small muted className="italic">
            Reviewed against an earlier prompt: {evalVerdictLabel(verdict)}
          </Type>
        ) : null}
      </Stack>
      <Stack
        direction="horizontal"
        gap={2}
        align="center"
        justify="center"
        className="w-full"
      >
        {(
          [
            { key: "agree", label: "Right", icon: ThumbsUp },
            { key: "disagree", label: "Wrong", icon: ThumbsDown },
          ] as const
        ).map((opt) => {
          const selected = currentAgreement === opt.key;
          const IconComponent = opt.icon;

          return (
            <Button
              key={opt.key}
              variant="secondary"
              size="sm"
              disabled={disabled}
              className={cn(
                "min-w-24 justify-center",
                agreementButtonClass(opt.key, selected),
              )}
              onClick={() => handleAgreement(opt.key)}
            >
              <Button.LeftIcon>
                <IconComponent className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>{opt.label}</Button.Text>
            </Button>
          );
        })}
      </Stack>
    </Stack>
  );
}

// Flatten a chat message's content (string, multimodal parts, or null) to text.
function messageText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((part) =>
        typeof part === "string"
          ? part
          : part && typeof (part as { text?: unknown }).text === "string"
            ? (part as { text: string }).text
            : "",
      )
      .join("")
      .trim();
  }
  return "";
}

// Summarize the first tool call on an assistant tool-request message.
function toolCallSummary(
  toolCalls: string | undefined,
): { name: string; args: string } | null {
  if (!toolCalls) return null;
  try {
    const calls: unknown = JSON.parse(toolCalls);
    const first = Array.isArray(calls) ? calls[0] : null;
    const fn = (first as { function?: { name?: string; arguments?: string } })
      ?.function;
    if (!fn) return null;
    return { name: fn.name ?? "", args: fn.arguments ?? "" };
  } catch {
    return null;
  }
}

function TranscriptMessage({
  message,
  verdict,
}: {
  message: ChatMessage;
  verdict: PromptGuardrailMessageVerdict | null;
}): JSX.Element {
  const tool = toolCallSummary(message.toolCalls);
  const isTool = message.role === "tool" || tool !== null;
  const roleLabel = tool
    ? "Tool call"
    : (ROLE_LABEL[message.role] ?? message.role);
  const body = tool ? tool.args : messageText(message.content);
  const flagged = verdict?.matched ?? false;

  return (
    <div
      className={cn(
        "rounded-md border p-2.5",
        flagged
          ? "border-warning/40 bg-warning/10"
          : "border-border/60 bg-muted/20",
      )}
    >
      <Stack direction="horizontal" gap={2} align="center" className="mb-1">
        <Badge variant="neutral">{roleLabel}</Badge>
        {tool?.name ? (
          <Type small mono muted className="truncate">
            {tool.name}
          </Type>
        ) : null}
      </Stack>
      {body ? (
        isTool ? (
          <Type small mono className="break-all">
            {body}
          </Type>
        ) : (
          <Type small>{body}</Type>
        )
      ) : (
        <Type small muted className="italic">
          (no text content)
        </Type>
      )}
      {verdict?.matched ? (
        <Stack
          direction="horizontal"
          gap={2}
          align="center"
          className="border-warning/30 mt-2 border-t pt-2"
        >
          <Icon
            name="triangle-alert"
            className="text-warning h-4 w-4 shrink-0"
          />
          <Type small className="flex-1">
            {verdict.rationale || "Flagged by the guardrail"}
          </Type>
          <Type small muted className="tabular-nums">
            {formatPct(verdict.confidence)}
          </Type>
        </Stack>
      ) : null}
    </div>
  );
}

// ── Standard policy editor (stepped: Detect → Scope → Action → Review) ────────
// Reuses PolicyCenter's detector/scope/action/audience building blocks and its
// payload mapping. `policy === null` means create mode.

function StandardPolicyEditor({
  policy,
}: {
  policy: RiskPolicy | null;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const { customRules } = useDetectionRulesStore();

  const [step, setStep] = useStepParam(STANDARD_STEPS);

  // Original values (edit mode) used for dirty tracking.
  const orig = useMemo(() => {
    if (!policy) return null;
    const cats = policyToCategories(policy.sources, policy.presidioEntities);
    if ((policy.customRuleIds ?? []).length > 0) cats.add("custom");
    return {
      name: policy.name,
      action: (policy.action as PolicyAction) ?? "flag",
      userMessage: policy.userMessage ?? "",
      scopeInclude: policy.scopeInclude ?? "",
      scopeExempt: policy.scopeExempt ?? "",
      messageTypes: policyMessageTypesForForm(policy.messageTypes),
      disabledRules: new Set(policy.disabledRules ?? []),
      customRuleIds: new Set(policy.customRuleIds ?? []),
      categories: cats,
      audienceType:
        policy.audienceType === "targeted"
          ? ("targeted" as const)
          : ("everyone" as const),
      audiencePrincipalUrns:
        policy.audienceType === "targeted"
          ? new Set(policy.audiencePrincipalUrns ?? [])
          : new Set<string>(),
    };
  }, [policy]);

  // ── Local form state, seeded from the policy (edit) or defaults (create). ──
  const [name, setName] = useState(policy?.name ?? "");
  const [selectedCategories, setSelectedCategories] = useState<
    Set<RuleCategory>
  >(() => orig?.categories ?? new Set<RuleCategory>());
  const [disabledRules, setDisabledRules] = useState<Set<string>>(
    () => new Set(policy?.disabledRules ?? []),
  );
  const [selectedCustomRuleIds, setSelectedCustomRuleIds] = useState<
    Set<string>
  >(() => new Set(policy?.customRuleIds ?? []));
  const [scopeInclude, setScopeInclude] = useState(policy?.scopeInclude ?? "");
  const [scopeExempt, setScopeExempt] = useState(policy?.scopeExempt ?? "");
  const [scopeMode, setScopeMode] = useState<"messageTypes" | "cel">(
    (policy?.scopeInclude ?? "").trim() !== "" ? "cel" : "messageTypes",
  );
  const [selectedMessageTypes, setSelectedMessageTypes] = useState<
    Set<PolicyMessageType>
  >(() => policyMessageTypesForForm(policy?.messageTypes));
  const [action, setAction] = useState<PolicyAction>(
    (policy?.action as PolicyAction) ?? "flag",
  );
  const [userMessage, setUserMessage] = useState(policy?.userMessage ?? "");
  const [audienceType, setAudienceType] = useState<"everyone" | "targeted">(
    policy?.audienceType === "targeted" ? "targeted" : "everyone",
  );
  const [audiencePrincipalUrns, setAudiencePrincipalUrns] = useState<
    Set<string>
  >(() =>
    policy?.audienceType === "targeted"
      ? new Set(policy.audiencePrincipalUrns ?? [])
      : new Set<string>(),
  );
  const [customizeCategory, setCustomizeCategory] =
    useState<RuleCategory | null>(null);
  const [detectionExpanded, setDetectionExpanded] = useState(true);

  // ── Derived state ──
  const includeCelStatus = useCelStatus(
    scopeMode === "cel" ? scopeInclude : "",
  );
  const exemptCelStatus = useCelStatus(scopeExempt);
  const flagOnlySelected = [...FLAG_ONLY_CATEGORIES].some((c) =>
    selectedCategories.has(c),
  );
  const hasEnabledDetector =
    selectedCustomRuleIds.size > 0 ||
    [...selectedCategories].some(
      (c) =>
        CATEGORY_LEVEL_DETECTORS.has(c) ||
        DETECTION_RULES[c]?.some((r) => !r.hidden && !disabledRules.has(r.id)),
    );
  const scopeMissing =
    scopeMode === "messageTypes"
      ? selectedMessageTypes.size === 0
      : scopeInclude.trim() === "";
  const applicationInvalid =
    (scopeMode === "cel" && includeCelStatus.kind === "error") ||
    exemptCelStatus.kind === "error";
  const audienceMissing =
    audienceType === "targeted" && audiencePrincipalUrns.size === 0;
  const saveBlocked =
    !hasEnabledDetector ||
    scopeMissing ||
    applicationInvalid ||
    audienceMissing;

  const dirty =
    !!orig &&
    (name !== orig.name ||
      action !== orig.action ||
      userMessage !== orig.userMessage ||
      scopeExempt !== orig.scopeExempt ||
      (scopeMode === "cel"
        ? scopeInclude !== orig.scopeInclude
        : orig.scopeInclude !== "") ||
      audienceType !== orig.audienceType ||
      !sameSet(selectedMessageTypes, orig.messageTypes) ||
      !sameSet(disabledRules, orig.disabledRules) ||
      !sameSet(selectedCustomRuleIds, orig.customRuleIds) ||
      !sameSet(selectedCategories, orig.categories) ||
      !sameSet(audiencePrincipalUrns, orig.audiencePrincipalUrns));

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskListPolicies(queryClient);
    },
  });
  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
      routes.policyCenter.goTo();
    },
  });
  const saving = updateMutation.isPending || createMutation.isPending;

  // Toggle a whole built-in detector category (clears its per-rule disables).
  // Flag-only categories force the policy action to flag.
  const toggleCategory = (cat: RuleCategory, checked: boolean) => {
    const rules = DETECTION_RULES[cat].filter((r) => !r.hidden);
    const nextCats = new Set(selectedCategories);
    const nextDisabled = new Set(disabledRules);
    if (checked) nextCats.add(cat);
    else nextCats.delete(cat);
    for (const rule of rules) nextDisabled.delete(rule.id);
    setSelectedCategories(nextCats);
    setDisabledRules(nextDisabled);
    if (checked && FLAG_ONLY_CATEGORIES.has(cat) && action === "block") {
      setAction("flag");
    }
  };
  const toggleDetector = (ruleId: string, checked: boolean) => {
    const next = new Set(selectedCustomRuleIds);
    if (checked) next.add(ruleId);
    else next.delete(ruleId);
    setSelectedCustomRuleIds(next);
  };

  // Build the full update/create body, mirroring PolicyCenter's standard branch.
  const save = () => {
    const {
      sources,
      presidioEntities,
      promptInjectionRules,
      disabledRules: payloadDisabled,
    } = categoriesToPayload(
      selectedCategories,
      disabledRules,
      pinnedHiddenRuleIds(policy?.presidioEntities),
    );
    const messageTypes =
      scopeMode === "cel"
        ? []
        : policyMessageTypesForPayload(selectedMessageTypes);
    const includeCel = scopeMode === "cel" ? scopeInclude.trim() : "";
    const exemptCel = scopeExempt.trim();
    // Destructive-tool sources reject action=block server-side; force flag.
    const resolvedAction =
      sources.includes("destructive_tool") && action === "block"
        ? "flag"
        : action;
    const principals =
      audienceType === "targeted" ? [...audiencePrincipalUrns] : [];
    const autoName = name.trim() === "";

    if (policy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: policy.id,
            name: name.trim() || policy.name,
            enabled: true,
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            messageTypes,
            scopeInclude: includeCel,
            scopeExempt: exemptCel,
            action: resolvedAction,
            audienceType,
            audiencePrincipalUrns: principals,
            autoName,
            userMessage,
          },
        },
      });
    } else {
      createMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            ...(autoName ? {} : { name: name.trim() }),
            enabled: true,
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            messageTypes,
            ...(includeCel ? { scopeInclude: includeCel } : {}),
            ...(exemptCel ? { scopeExempt: exemptCel } : {}),
            action: resolvedAction,
            audienceType,
            audiencePrincipalUrns: principals,
            autoName,
            ...(userMessage.trim() ? { userMessage } : {}),
          },
        },
      });
    }
  };

  const header = (
    <PolicyHeader
      kind="standard"
      policy={policy}
      name={name}
      onNameChange={setName}
      dirty={dirty}
      saving={saving}
      actionDisabled={saveBlocked}
      onSubmit={() => save()}
      onCreate={save}
    />
  );

  return (
    <>
      <StepperShell
        header={header}
        steps={STANDARD_STEPS}
        current={step}
        onStep={setStep}
      >
        {step === 0 && (
          <Card>
            <SectionHeader description="Turn on detector categories and attach your organization's custom rules." />
            <Stack gap={5}>
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <Label className="text-sm font-medium">Built-in rules</Label>
                  <span className="text-muted-foreground text-xs">
                    {
                      ALL_CATEGORIES.filter((c) => selectedCategories.has(c))
                        .length
                    }{" "}
                    on
                  </span>
                </div>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {ALL_CATEGORIES.map((cat) => (
                    <DetectorCard
                      key={cat}
                      category={cat}
                      selected={selectedCategories.has(cat)}
                      disabledRules={disabledRules}
                      onToggle={(checked) => toggleCategory(cat, checked)}
                      onCustomize={() => setCustomizeCategory(cat)}
                    />
                  ))}
                </div>
              </div>
              {customRules.length > 0 && (
                <RuleSelectList
                  title="Custom Rules"
                  description={
                    <>
                      Attach your organization's custom rules as{" "}
                      <span className="text-foreground font-medium">
                        detectors
                      </span>{" "}
                      — a match records a finding.
                    </>
                  }
                  idPrefix="detector"
                  customRules={customRules}
                  selectedRuleIds={selectedCustomRuleIds}
                  onToggleRule={toggleDetector}
                  expanded={detectionExpanded}
                  onToggle={() => setDetectionExpanded((v) => !v)}
                />
              )}
              {!hasEnabledDetector && (
                <Type small className="text-destructive">
                  Turn on at least one detector or attach a custom rule.
                </Type>
              )}
            </Stack>
          </Card>
        )}

        {step === 1 && (
          <ScopeStep
            description="Apply everywhere, or narrow the scope to reduce noise and cost."
            messageTypes={selectedMessageTypes}
            setMessageTypes={setSelectedMessageTypes}
            scopeMode={scopeMode}
            setScopeMode={setScopeMode}
            scopeInclude={scopeInclude}
            setScopeInclude={setScopeInclude}
            scopeExempt={scopeExempt}
            setScopeExempt={setScopeExempt}
          />
        )}

        {step === 2 && (
          <ActionStep
            action={action}
            setAction={setAction}
            audienceType={audienceType}
            setAudienceType={setAudienceType}
            audiencePrincipalUrns={audiencePrincipalUrns}
            setAudiencePrincipalUrns={setAudiencePrincipalUrns}
            userMessage={userMessage}
            setUserMessage={setUserMessage}
            flagOnlySelected={flagOnlySelected}
          />
        )}

        {step === 3 && (
          <StandardReview
            name={name}
            categories={selectedCategories}
            customRuleCount={selectedCustomRuleIds.size}
            scopeMode={scopeMode}
            selectedMessageTypes={selectedMessageTypes}
            scopeInclude={scopeInclude}
            scopeExempt={scopeExempt}
            action={action}
            audienceType={audienceType}
            audiencePrincipalCount={audiencePrincipalUrns.size}
          />
        )}

        <StepNav step={step} count={STANDARD_STEPS.length} onStep={setStep} />
      </StepperShell>

      {customizeCategory && (
        <CustomizeRulesSheet
          category={customizeCategory}
          selectedCategories={selectedCategories}
          setSelectedCategories={setSelectedCategories}
          disabledRules={disabledRules}
          setDisabledRules={setDisabledRules}
          onClose={() => setCustomizeCategory(null)}
        />
      )}
    </>
  );
}

// Read-only recap of the standard policy configuration on the Review step.
function StandardReview({
  name,
  categories,
  customRuleCount,
  scopeMode,
  selectedMessageTypes,
  scopeInclude,
  scopeExempt,
  action,
  audienceType,
  audiencePrincipalCount,
}: {
  name: string;
  categories: Set<RuleCategory>;
  customRuleCount: number;
  scopeMode: "messageTypes" | "cel";
  selectedMessageTypes: Set<PolicyMessageType>;
  scopeInclude: string;
  scopeExempt: string;
  action: PolicyAction;
  audienceType: "everyone" | "targeted";
  audiencePrincipalCount: number;
}): JSX.Element {
  const detectorLabels = [...categories]
    .filter((c) => c !== "custom")
    .map((c) => RULE_CATEGORY_META[c].label);
  if (customRuleCount > 0) {
    detectorLabels.push(
      `${customRuleCount} custom rule${customRuleCount === 1 ? "" : "s"}`,
    );
  }

  const scopeText =
    scopeMode === "cel"
      ? scopeInclude.trim() || "All messages matching a CEL expression"
      : [...selectedMessageTypes]
          .map((t) => POLICY_MESSAGE_TYPE_META[t].label)
          .join(", ") || "No message types selected";

  return (
    <Card>
      <SectionHeader description="Confirm the configuration before creating the policy." />
      <Stack gap={4}>
        <SummaryRow label="Name">
          <Type small>{name.trim() || "Auto-generated from detectors"}</Type>
        </SummaryRow>
        <SummaryRow label="Detectors">
          {detectorLabels.length > 0 ? (
            <div className="flex flex-wrap justify-end gap-1.5">
              {detectorLabels.map((l) => (
                <Badge key={l} variant="neutral">
                  {l}
                </Badge>
              ))}
            </div>
          ) : (
            <Type small muted>
              None selected
            </Type>
          )}
        </SummaryRow>
        <SummaryRow label="Scope">
          <Type small className="text-right">
            {scopeText}
          </Type>
        </SummaryRow>
        {scopeExempt.trim() ? (
          <SummaryRow label="Exemptions">
            <Type small mono className="text-right break-all">
              {scopeExempt.trim()}
            </Type>
          </SummaryRow>
        ) : null}
        <SummaryRow label="Action">
          <Badge variant={action === "block" ? "warning" : "neutral"}>
            {action === "block" ? "Block" : "Flag"}
          </Badge>
        </SummaryRow>
        <SummaryRow label="Audience">
          <Type small>
            {audienceType === "targeted"
              ? `${audiencePrincipalCount} targeted principal${
                  audiencePrincipalCount === 1 ? "" : "s"
                }`
              : "Everyone"}
          </Type>
        </SummaryRow>
      </Stack>
    </Card>
  );
}

function SummaryRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <Stack
      direction="horizontal"
      justify="space-between"
      align="center"
      gap={4}
      className="border-border/60 border-b pb-3 last:border-b-0 last:pb-0"
    >
      <Type small muted className="shrink-0">
        {label}
      </Type>
      <div className="min-w-0">{children}</div>
    </Stack>
  );
}

// ── helpers ─────────────────────────────────────────────────────────────────

function sameSet<T>(a: Set<T>, b: Set<T>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

function formatRelative(date: Date): string {
  const secs = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
  if (secs < 60) return "just now";
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function formatPct(n: number): string {
  return `${Math.round(n * 100)}%`;
}
