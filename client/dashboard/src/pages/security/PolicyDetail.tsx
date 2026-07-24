import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ShadowMCPPolicyServerSelector } from "@/components/shadow-mcp/ShadowMCPPolicyServerSelector";
import {
  initialShadowMCPPolicyURLs,
  invalidateShadowMCPPolicyInventory,
  useShadowMCPPolicyInventory,
} from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useProject } from "@/contexts/Auth";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { useRiskCategories } from "@gram/client/react-query/riskCategories.js";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskPoliciesUpdateMutation } from "@gram/client/react-query/riskPoliciesUpdate.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import {
  invalidateAllRiskPoliciesGet,
  useRiskPoliciesGet,
} from "@gram/client/react-query/riskPoliciesGet.js";
import { riskEvalsEvaluate } from "@gram/client/funcs/riskEvalsEvaluate.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { RiskCategoryDefinition } from "@gram/client/models/components/riskcategorydefinition.js";
import type { RiskDetectionScope } from "@gram/client/models/components/riskdetectionscope.js";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import {
  keepPreviousData,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  Check,
  Code,
  Info,
  Loader2,
  Pencil,
  Shield,
  Sparkles,
  ThumbsDown,
  ThumbsUp,
  TriangleAlert,
  X,
} from "lucide-react";
import {
  Fragment,
  type ReactNode,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useParams } from "react-router";
import { useQueryState } from "nuqs";
import {
  isBlockingShadowMCPPolicy,
  isShadowMCPBlockConfiguration,
  shadowMCPAllowedURLsForMutation,
  shadowMCPSelectionBaselineForUpdate,
  shadowMCPSelectionIsDirty,
  shadowMCPSelectionIsInitialized,
} from "./policy-shadow-mcp-setup";
import { type Step } from "@/pages/setup/components/onboarding-stepper";
import {
  DETECTION_RULES,
  RULE_CATEGORY_META,
  type PolicyAction,
  type RuleCategory,
} from "./policy-data";
import {
  ActionPicker,
  CustomizeRulesSheet,
  PolicyAudiencePicker,
  RuleSelectList,
} from "./PolicyCenter";
import { DetectorCard } from "./DetectorCard";
import { builtInRuleDisabledReason } from "./policy-built-in-rule-exclusivity";
import {
  ALL_CATEGORIES,
  CATEGORY_LEVEL_DETECTORS,
  FLAG_ONLY_CATEGORIES,
  PRESIDIO_CATEGORIES,
  SCOPE_EXEMPT_CEL_EXAMPLES,
  SCOPE_INCLUDE_CEL_EXAMPLES,
  categoriesToPayload,
  parseApprovedEmailDomains,
  pinnedHiddenRuleIds,
  policyToCategories,
} from "./policy-form";
import { SeverityBadge } from "./risk-ui";
import { CelExpressionField } from "./cel-field";
import { CelReferenceSheet } from "./cel-reference";
import { CelTrafficPreview } from "./cel-traffic-preview";
import { useCelEngine } from "./use-cel-engine";
import type { CelEngine, CelMessage } from "./cel-wasm";
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
import type { PromptGuardrailEvalResult } from "@gram/client/models/components/promptguardrailevalresult.js";
import type { PromptGuardrailMessageVerdict } from "@gram/client/models/components/promptguardrailmessageverdict.js";
import {
  ChatTranscript,
  type RowContext,
  type TranscriptPagination,
} from "@/pages/chatLogs/ChatTranscript";
import {
  buildDisplayItems,
  buildTranscript,
} from "@/pages/chatLogs/transcript";
import { useChatTranscript } from "@/pages/chatLogs/useChatTranscript";
import { formatUsageCost } from "@/pages/chatLogs/claudeUsage";

// Judge models offered in the workbench (mirrors PolicyCenter's list; the
// picker is intentionally small until the model catalog is centralized).
// Sentinel for the "use server default" model option — Radix Select forbids an
// empty-string item value, so "" is mapped through this and back on change.
const DEFAULT_MODEL_VALUE = "__default__";

// Gemini 3.5 Flash is deliberately absent: the judge disables reasoning
// (`reasoning.effort: "none"`), which the Gemini 3.5 generation rejects with a
// 400 — every evaluation on it would fail into the policy's error mode.
const JUDGE_MODELS: { value: string; label: string }[] = [
  { value: "", label: "Default (Gemini 3.1 Flash Lite)" },
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
    id: "sensitivity",
    title: "Sensitivity",
    description: "Tune detection confidence.",
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
      <Page.Body className="min-h-full">
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
        <Page.Body className="min-h-full">
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
        <Page.Body className="min-h-full">
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
              Start with a built-in detector policy or define criteria in plain
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
              <Type className="font-medium">Built-in detector</Type>
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
    // standard page width). flex-1 (paired with min-h-full on Page.Body) lets
    // the content area grow so the Back/Continue footer keeps a stable
    // position instead of jumping as each step's content height changes.
    <Stack gap={6} className="w-full flex-1">
      {header}
      <div className="bg-muted/20 rounded-lg border px-4 py-3">
        <HorizontalStepper steps={steps} current={current} onStep={onStep} />
      </div>
      <Stack gap={6} className="flex-1">
        {children}
      </Stack>
      <StepNav step={current} count={steps.length} onStep={onStep} />
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
    kind === "prompt" ? "Prompt-based (LLM judge)" : "Built-in detector";
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
  const [scopeOverrides, setScopeOverrides] = useState<
    Map<string, ScopeOverride>
  >(() => scopeOverridesFromPolicy(policy?.detectionScopes));
  const [action, setAction] = useState<PolicyAction>(policy?.action ?? "flag");
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
  const [score, setScore] = useState(policy?.score ?? 5);
  const [reviewVerdictFilter, setReviewVerdictFilter] =
    useState<EvalVerdict | null>(null);

  const dirty =
    !!policy &&
    (name !== policy.name ||
      prompt !== (policy.prompt ?? "") ||
      model !== (policy.modelConfig?.model ?? "") ||
      temperature !== (policy.modelConfig?.temperature ?? 0) ||
      failOpen !== (policy.modelConfig?.failOpen ?? true) ||
      !sameScopeOverrides(
        scopeOverrides,
        scopeOverridesFromPolicy(policy.detectionScopes),
      ) ||
      action !== (policy.action ?? "flag") ||
      userMessage !== (policy.userMessage ?? "") ||
      score !== (policy.score ?? 5) ||
      audienceType !==
        (policy.audienceType === "targeted" ? "targeted" : "everyone") ||
      !sameSet(
        audiencePrincipalUrns,
        new Set(
          policy.audienceType === "targeted"
            ? (policy.audiencePrincipalUrns ?? [])
            : [],
        ),
      ));

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

  const actionPayload = () => ({
    action,
    audienceType,
    audiencePrincipalUrns:
      audienceType === "targeted" ? [...audiencePrincipalUrns] : [],
  });

  // Blank name → the backend auto-generates one from the guardrail (mirrors
  // standard policies auto-naming from detectors).
  const autoName = name.trim() === "";
  const promptPolicyCategories = useMemo(
    () => new Set<RuleCategory>(["prompt_policy"]),
    [],
  );
  const detectionScopesPayloadForPrompt = () =>
    detectionScopesPayload(promptPolicyCategories, scopeOverrides);

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
          detectionScopes: detectionScopesPayloadForPrompt(),
          ...actionPayload(),
          userMessage,
          score,
          autoName,
        },
      },
    });
  };

  const create = () => {
    const detectionScopes = detectionScopesPayloadForPrompt();
    createMutation.mutate({
      request: {
        createRiskPolicyRequestBody: {
          policyType: "prompt_based",
          ...(autoName ? {} : { name: name.trim() }),
          enabled: true,
          prompt,
          modelConfig: { model: model || undefined, temperature, failOpen },
          ...(detectionScopes.length > 0 ? { detectionScopes } : {}),
          ...actionPayload(),
          ...(userMessage.trim() ? { userMessage } : {}),
          score,
          autoName,
        },
      },
    });
  };

  const canCreate = prompt.trim().length > 0;

  // Stable guardrail snapshot for eval query keys. The replay uses the
  // prompt_policy category's effective detection scope (the override when
  // set, the recommendation otherwise) so eval verdicts match scan behavior.
  const categoriesQuery = useRiskCategories();
  const promptPolicyDef = categoriesQuery.data?.categories?.find(
    (c) => c.key === "prompt_policy",
  );
  const effectiveScope = scopeOverrides.get("prompt_policy") ?? {
    scopeInclude: promptPolicyDef?.recommendedScopeInclude ?? "",
    scopeExempt: promptPolicyDef?.recommendedScopeExempt ?? "",
  };
  // A preserved legacy policy-level scope still intersects the category scope
  // in production (scanner: includes AND, exempts OR), so compose it here too.
  const guardrail = useMemo<Guardrail>(
    () => ({
      prompt,
      model,
      temperature,
      failOpen,
      messageTypes: policy?.messageTypes ?? [],
      scopeInclude: intersectScopeExprs(
        policy?.scopeInclude ?? "",
        effectiveScope.scopeInclude,
      ),
      scopeExempt: unionScopeExprs(
        policy?.scopeExempt ?? "",
        effectiveScope.scopeExempt,
      ),
    }),
    [
      prompt,
      model,
      temperature,
      failOpen,
      policy,
      effectiveScope.scopeInclude,
      effectiveScope.scopeExempt,
    ],
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
          selectedCategories={promptPolicyCategories}
          scopeOverrides={scopeOverrides}
          setScopeOverrides={setScopeOverrides}
          legacyPolicy={policy}
        />
      )}

      {step === 2 && (
        <EvalTuner
          guardrail={guardrail}
          onPromptChange={setPrompt}
          verdicts={evalReview.verdicts}
          setVerdict={evalReview.setVerdict}
          reviewVerdictFilter={reviewVerdictFilter}
          setReviewVerdictFilter={setReviewVerdictFilter}
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
          score={score}
          setScore={setScore}
        />
      )}

      {step === 4 && (
        <PromptReview
          prompt={prompt}
          model={model}
          temperature={temperature}
          failOpen={failOpen}
          customizedScopeCount={scopeOverrides.size}
          action={action}
          score={score}
          audienceType={audienceType}
          audiencePrincipalCount={audiencePrincipalUrns.size}
          verdicts={evalReview.verdicts}
          activeVerdict={reviewVerdictFilter}
          onVerdictSelect={(verdict) => {
            setReviewVerdictFilter(verdict);
            handleStep(2);
          }}
        />
      )}
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
  selectedCategories,
  scopeOverrides,
  setScopeOverrides,
  legacyPolicy,
}: {
  description: string;
  selectedCategories: Set<RuleCategory>;
  scopeOverrides: Map<string, ScopeOverride>;
  setScopeOverrides: (next: Map<string, ScopeOverride>) => void;
  legacyPolicy?: RiskPolicy | null;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description={description} />
      <Stack gap={5}>
        <RecommendedScopesPanel
          selectedCategories={selectedCategories}
          scopeOverrides={scopeOverrides}
          setScopeOverrides={setScopeOverrides}
        />
        <LegacyScopeNotice policy={legacyPolicy} />
      </Stack>
    </Card>
  );
}

// Read-only reminder for policies that still carry a policy-level scope from
// before category detection scopes became the only scoping surface. The
// dashboard no longer edits these fields; a migration will fold them into
// category scopes.
function LegacyScopeNotice({
  policy,
}: {
  policy?: RiskPolicy | null;
}): JSX.Element | null {
  if (!policy) return null;
  const parts: string[] = [];
  if ((policy.messageTypes ?? []).length > 0) {
    parts.push(`message types: ${(policy.messageTypes ?? []).join(", ")}`);
  }
  if ((policy.scopeInclude ?? "").trim() !== "") {
    parts.push(`include: ${(policy.scopeInclude ?? "").trim()}`);
  }
  if ((policy.scopeExempt ?? "").trim() !== "") {
    parts.push(`exempt: ${(policy.scopeExempt ?? "").trim()}`);
  }
  if (parts.length === 0) return null;
  return (
    <div className="border-border bg-muted/20 rounded-md border px-3 py-2">
      <Type small muted>
        A legacy policy-level scope still narrows this policy in addition to the
        category scopes above ({parts.join("; ")}). It is preserved as-is and
        will be migrated into category scopes.
      </Type>
    </div>
  );
}

function RecommendedScopesPanel({
  selectedCategories,
  scopeOverrides,
  setScopeOverrides,
}: {
  selectedCategories: Set<RuleCategory>;
  scopeOverrides: Map<string, ScopeOverride>;
  setScopeOverrides: (next: Map<string, ScopeOverride>) => void;
}): JSX.Element | null {
  // Handled inline (retry below) instead of the route error boundary.
  const categoriesQuery = useRiskCategories(undefined, undefined, {
    throwOnError: false,
  });

  const rows = useMemo(() => {
    if (!categoriesQuery.data?.categories) return [];
    return categoriesQuery.data.categories
      .filter((category) =>
        selectedCategories.has(category.key as RuleCategory),
      )
      .filter((category) => hasDisplayableRecommendedScope(category));
  }, [categoriesQuery.data?.categories, selectedCategories]);

  if (categoriesQuery.isLoading) {
    return (
      <Type small muted className="flex items-center gap-2">
        <Loader2 className="size-4 animate-spin" />
        Loading detection scopes…
      </Type>
    );
  }
  if (categoriesQuery.isError) {
    return (
      <div className="flex items-center gap-3">
        <Type small muted>
          Failed to load detection scopes.
        </Type>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void categoriesQuery.refetch()}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  }
  if (rows.length === 0) {
    return null;
  }

  return (
    <div className="space-y-3">
      <div className="flex items-end justify-between gap-3">
        <div className="space-y-1">
          <Label className="text-sm font-medium">Detection scopes</Label>
          <p className="text-muted-foreground text-xs">
            Each category scans the highlighted surfaces. Click a surface to
            customize; a custom scope replaces the recommendation.
          </p>
        </div>
        <CelReferenceSheet />
      </div>
      <div className="space-y-2">
        {rows.map((category) => (
          <RecommendedScopeRow
            key={category.key}
            category={category}
            override={scopeOverrides.get(category.key)}
            onOverrideChange={(override) => {
              const next = new Map(scopeOverrides);
              if (override === null) next.delete(category.key);
              else next.set(category.key, override);
              setScopeOverrides(next);
            }}
          />
        ))}
      </div>
    </div>
  );
}

// The four message surfaces a detection scope selects over, in transcript
// order. Chip editing serializes back to canonical kind expressions.
const SCOPE_SURFACES = [
  { kind: "user_message", label: "User" },
  { kind: "tool_request", label: "Tool requests" },
  { kind: "tool_response", label: "Tool responses" },
  { kind: "assistant_message", label: "Assistant" },
] as const;
type ScopeSurfaceKind = (typeof SCOPE_SURFACES)[number]["kind"];
const ALL_SURFACE_KINDS: ScopeSurfaceKind[] = SCOPE_SURFACES.map((s) => s.kind);

// Parses an expression made solely of `kind == "..."` terms ORed together;
// null for anything else (a real CEL scope the chips cannot represent).
function kindsFromExpr(expr: string): Set<ScopeSurfaceKind> | null {
  const trimmed = expr.trim();
  const out = new Set<ScopeSurfaceKind>();
  if (trimmed === "") return out;
  for (const part of trimmed.split("||")) {
    const match = /^\(?\s*kind\s*==\s*"(\w+)"\s*\)?$/.exec(part.trim());
    const kind = match?.[1] as ScopeSurfaceKind | undefined;
    if (!kind || !ALL_SURFACE_KINDS.includes(kind)) return null;
    out.add(kind);
  }
  return out;
}

// The surfaces a scope admits, or null when it is not a pure surface
// expression. An empty include means every surface.
function surfacesFromScope(
  include: string,
  exempt: string,
): Set<ScopeSurfaceKind> | null {
  const included = kindsFromExpr(include);
  const exempted = kindsFromExpr(exempt);
  if (included === null || exempted === null) return null;
  const base =
    included.size === 0
      ? new Set<ScopeSurfaceKind>(ALL_SURFACE_KINDS)
      : included;
  return new Set([...base].filter((kind) => !exempted.has(kind)));
}

type SurfaceState = "in" | "out" | "conditional";

// Canonical probe messages per surface. tool_request gets a write probe and an
// all-read-only probe so tool-conditional scopes (e.g. a read-only allowlist)
// register as "conditional" rather than a hard yes/no.
const SURFACE_PROBES: Record<ScopeSurfaceKind, CelMessage[]> = {
  user_message: [{ type: "user_message", content: "sample text" }],
  assistant_message: [{ type: "assistant_message", content: "sample text" }],
  tool_response: [{ type: "tool_response", content: "sample text" }],
  tool_request: [
    {
      type: "tool_request",
      content: "",
      tools: [
        { name: "Bash", server: "", function: "Bash", args: '{"command":"x"}' },
      ],
    },
    {
      type: "tool_request",
      content: "",
      tools: [{ name: "Read", server: "", function: "Read", args: "{}" }],
    },
  ],
};

// Per-surface footprint for scopes the chips cannot represent exactly
// (granular recommendations), derived by evaluating the scope against the
// probes: in scope for every probe, none, or only some ("conditional").
function surfaceStatesFromProbes(
  engine: CelEngine,
  include: string,
  exempt: string,
): Record<ScopeSurfaceKind, SurfaceState> | null {
  const inc = include.trim();
  const exc = exempt.trim();
  if (inc !== "" && !engine.compile(inc).ok) return null;
  if (exc !== "" && !engine.compile(exc).ok) return null;
  const out = {} as Record<ScopeSurfaceKind, SurfaceState>;
  for (const kind of ALL_SURFACE_KINDS) {
    const verdicts: boolean[] = [];
    for (const probe of SURFACE_PROBES[kind]) {
      let inScope = true;
      if (inc !== "") {
        const result = engine.evalDetection(inc, probe);
        if (!result.ok) return null;
        inScope = result.matched;
      }
      if (inScope && exc !== "") {
        const result = engine.evalDetection(exc, probe);
        if (!result.ok) return null;
        if (result.matched) inScope = false;
      }
      verdicts.push(inScope);
    }
    out[kind] = verdicts.every(Boolean)
      ? "in"
      : verdicts.some(Boolean)
        ? "conditional"
        : "out";
  }
  return out;
}

// Forces one surface fully in or out of a scope the chips cannot express
// exactly, by wrapping the existing predicates rather than rewriting them:
// the rest of the scope's conditions are preserved verbatim.
function scopeWithSurface(
  scope: ScopeOverride,
  kind: ScopeSurfaceKind,
  on: boolean,
): ScopeOverride {
  const include = scope.scopeInclude.trim();
  const exempt = scope.scopeExempt.trim();
  const kindEq = `kind == "${kind}"`;
  const kindNe = `kind != "${kind}"`;
  if (on) {
    return {
      scopeInclude: include === "" ? "" : `(${include}) || ${kindEq}`,
      scopeExempt: exempt === "" ? "" : `(${exempt}) && ${kindNe}`,
    };
  }
  return {
    scopeInclude: include === "" ? "" : `(${include}) && ${kindNe}`,
    scopeExempt: exempt === "" ? kindEq : `(${exempt}) || ${kindEq}`,
  };
}

// Canonical scope for a surface set: every surface = unrestricted, one
// missing = exempt it, otherwise include the chosen surfaces.
function scopeFromSurfaces(surfaces: Set<ScopeSurfaceKind>): ScopeOverride {
  if (surfaces.size >= ALL_SURFACE_KINDS.length) {
    return { scopeInclude: "", scopeExempt: "" };
  }
  const missing = ALL_SURFACE_KINDS.filter((kind) => !surfaces.has(kind));
  if (missing.length === 1) {
    return { scopeInclude: "", scopeExempt: `kind == "${missing[0]}"` };
  }
  return {
    scopeInclude: ALL_SURFACE_KINDS.filter((kind) => surfaces.has(kind))
      .map((kind) => `kind == "${kind}"`)
      .join(" || "),
    scopeExempt: "",
  };
}

function RecommendedScopeRow({
  category,
  override,
  onOverrideChange,
}: {
  category: RiskCategoryDefinition;
  override: ScopeOverride | undefined;
  onOverrideChange: (override: ScopeOverride | null) => void;
}): JSX.Element {
  const [celOpen, setCelOpen] = useState(false);
  const engineState = useCelEngine();
  const engine = engineState.status === "ready" ? engineState.engine : null;

  if (!category.recommendedScopeApplicable) {
    return (
      <div className="border-border bg-muted/20 flex items-center justify-between gap-3 rounded-md border px-3 py-2.5">
        <div className="flex min-w-0 items-center gap-2">
          <Type small className="font-medium">
            {category.label}
          </Type>
          <ScopeRationaleHint rationale={category.recommendedScopeRationale} />
        </div>
        <Badge variant="neutral">Session-scoped</Badge>
      </div>
    );
  }

  const activeScope = override ?? {
    scopeInclude: category.recommendedScopeInclude,
    scopeExempt: category.recommendedScopeExempt,
  };
  const activeSurfaces = surfacesFromScope(
    activeScope.scopeInclude,
    activeScope.scopeExempt,
  );
  const granularChips = !celOpen && activeSurfaces === null;
  const editorsOpen = celOpen && override !== undefined;

  const toggleSurface = (kind: ScopeSurfaceKind) => {
    if (!activeSurfaces) return;
    const next = new Set(activeSurfaces);
    if (next.has(kind)) {
      if (next.size === 1) return;
      next.delete(kind);
    } else {
      next.add(kind);
    }
    onOverrideChange(scopeFromSurfaces(next));
  };

  return (
    <div className="border-border rounded-md border px-3 py-2.5">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <Type small className="font-medium">
            {category.label}
          </Type>
          <ScopeRationaleHint rationale={category.recommendedScopeRationale} />
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Badge variant="neutral">
            {override === undefined ? "Recommended" : "Custom"}
          </Badge>
          {override !== undefined && (
            <button
              type="button"
              onClick={() => {
                setCelOpen(false);
                onOverrideChange(null);
              }}
              className="text-muted-foreground hover:text-foreground text-xs underline"
            >
              Reset
            </button>
          )}
        </div>
      </div>

      {!celOpen && activeSurfaces && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {SCOPE_SURFACES.map(({ kind, label }) => {
            const active = activeSurfaces.has(kind);
            return (
              <button
                key={kind}
                type="button"
                onClick={() => toggleSurface(kind)}
                aria-pressed={active}
                className={cn(
                  "rounded-full border px-2.5 py-0.5 text-xs transition-colors",
                  active
                    ? "border-foreground bg-foreground text-background"
                    : "border-border text-muted-foreground hover:text-foreground",
                )}
              >
                {label}
              </button>
            );
          })}
          <SimpleTooltip tooltip="Switch to CEL expressions for granular scoping: match on tool names, servers, or message content instead of whole surfaces.">
            <button
              type="button"
              onClick={() => {
                if (override === undefined) {
                  onOverrideChange({ ...activeScope });
                }
                setCelOpen(true);
              }}
              className="border-border text-muted-foreground hover:text-foreground hover:border-foreground/40 ml-1 flex items-center gap-1 rounded-full border border-dashed px-2.5 py-0.5 text-xs transition-colors"
            >
              <Code className="h-3 w-3" />
              Granular scope
            </button>
          </SimpleTooltip>
        </div>
      )}

      {granularChips && (
        <GranularRecommendationChips
          engine={engine}
          scope={activeScope}
          onToggleSurface={(kind, on) =>
            onOverrideChange(scopeWithSurface(activeScope, kind, on))
          }
          onCustomize={() => {
            if (override === undefined) {
              onOverrideChange({ ...activeScope });
            }
            setCelOpen(true);
          }}
        />
      )}

      {editorsOpen && override !== undefined && (
        <div className="mt-3 space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs font-medium">
              Detect on messages matching
            </Label>
            <CelExpressionField
              value={override.scopeInclude}
              onChange={(value) =>
                onOverrideChange({ ...override, scopeInclude: value })
              }
              examples={SCOPE_INCLUDE_CEL_EXAMPLES}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs font-medium">
              Exempt messages matching
            </Label>
            <CelExpressionField
              value={override.scopeExempt}
              onChange={(value) =>
                onOverrideChange({ ...override, scopeExempt: value })
              }
              examples={SCOPE_EXEMPT_CEL_EXAMPLES}
            />
            <Type small muted>
              Empty include and exempt scans every message surface. This scope
              replaces the recommendation; future recommendation updates will
              not apply.
            </Type>
          </div>
          <div className="border-border border-t pt-3">
            <CelTrafficPreview
              includeExpr={override.scopeInclude}
              exemptExpr={override.scopeExempt}
              mode="scope"
            />
          </div>
        </div>
      )}
    </div>
  );
}

// A scope the chips cannot express exactly (e.g. a tool-name allowlist inside
// tool requests): tri-state chips derived from the scope's probe footprint.
// Clicking a chip forces that surface fully in or out while the rest of the
// expression is preserved; conditional chips click to fully in. The exact
// expression stays behind Granular scope. Falls back to the raw expression
// while the engine loads.
function GranularRecommendationChips({
  engine,
  scope,
  onToggleSurface,
  onCustomize,
}: {
  engine: CelEngine | null;
  scope: ScopeOverride;
  onToggleSurface: (kind: ScopeSurfaceKind, on: boolean) => void;
  onCustomize: () => void;
}): JSX.Element {
  const states = engine
    ? surfaceStatesFromProbes(engine, scope.scopeInclude, scope.scopeExempt)
    : null;
  const scannedCount = states
    ? Object.values(states).filter((s) => s !== "out").length
    : 0;

  return (
    <div className="mt-2 space-y-2">
      {states ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {SCOPE_SURFACES.map(({ kind, label }) => {
            const state = states[kind];
            // Tri-state checkbox semantics: out and conditional click to
            // fully in; in clicks to out (unless it is the last surface).
            const nextOn = state !== "in";
            const lastSurface = state !== "out" && scannedCount <= 1;
            const chip = (
              <button
                key={kind}
                type="button"
                aria-pressed={state !== "out"}
                onClick={() => {
                  if (!nextOn && lastSurface) return;
                  onToggleSurface(kind, nextOn);
                }}
                className={cn(
                  "rounded-full border px-2.5 py-0.5 text-xs transition-colors",
                  state === "in" &&
                    "border-foreground bg-foreground text-background",
                  state === "conditional" &&
                    "border-foreground/60 text-foreground border-dashed",
                  state === "out" &&
                    "border-border text-muted-foreground hover:text-foreground",
                )}
              >
                {label}
                {state === "conditional" && "*"}
              </button>
            );
            return state === "conditional" ? (
              <SimpleTooltip
                key={kind}
                tooltip="Conditionally in scope: only some messages on this surface are scanned. Click to scan all of them, or open Granular scope for the exact expression."
              >
                {chip}
              </SimpleTooltip>
            ) : (
              chip
            );
          })}
          <SimpleTooltip tooltip="View and edit the exact CEL expressions behind this scope.">
            <button
              type="button"
              onClick={onCustomize}
              className="border-border text-muted-foreground hover:text-foreground hover:border-foreground/40 ml-1 flex items-center gap-1 rounded-full border border-dashed px-2.5 py-0.5 text-xs transition-colors"
            >
              <Code className="h-3 w-3" />
              Granular scope
            </button>
          </SimpleTooltip>
        </div>
      ) : (
        <div className="space-y-2">
          <RecommendedScopeCode
            include={scope.scopeInclude}
            exempt={scope.scopeExempt}
          />
          <button
            type="button"
            onClick={onCustomize}
            className="text-muted-foreground hover:text-foreground text-xs underline"
          >
            Customize
          </button>
        </div>
      )}
    </div>
  );
}

function ScopeRationaleHint({
  rationale,
}: {
  rationale: string;
}): JSX.Element | null {
  if (rationale.trim() === "") return null;
  return (
    <SimpleTooltip tooltip={rationale}>
      <Info className="text-muted-foreground size-3.5 shrink-0" />
    </SimpleTooltip>
  );
}

function RecommendedScopeCode({
  include,
  exempt,
}: {
  include: string;
  exempt: string;
}): JSX.Element {
  return (
    <div className="space-y-1.5">
      {include.trim() !== "" && (
        <RecommendedScopeCodeLine label="Include" expr={include} />
      )}
      {exempt.trim() !== "" && (
        <RecommendedScopeCodeLine label="Exempt" expr={exempt} />
      )}
    </div>
  );
}

function RecommendedScopeCodeLine({
  label,
  expr,
}: {
  label: string;
  expr: string;
}): JSX.Element {
  return (
    <div className="grid gap-1 sm:grid-cols-[4.5rem_minmax(0,1fr)]">
      <span className="text-muted-foreground text-xs">{label}</span>
      <pre className="bg-muted/50 text-muted-foreground overflow-x-auto rounded px-2 py-1 font-mono text-[11px] leading-tight whitespace-pre">
        {expr}
      </pre>
    </div>
  );
}

function hasDisplayableRecommendedScope(
  category: RiskCategoryDefinition,
): boolean {
  if (!category.recommendedScopeApplicable) return true;
  return (
    category.recommendedScopeInclude.trim() !== "" ||
    category.recommendedScopeExempt.trim() !== ""
  );
}

// A category with an entry here has its recommendation replaced by the
// user-authored scope; both fields empty scans every message surface.
type ScopeOverride = { scopeInclude: string; scopeExempt: string };

function scopeOverridesFromPolicy(
  scopes: RiskDetectionScope[] | undefined,
): Map<string, ScopeOverride> {
  return new Map(
    (scopes ?? []).map((s) => [
      s.category,
      { scopeInclude: s.scopeInclude ?? "", scopeExempt: s.scopeExempt ?? "" },
    ]),
  );
}

function sameScopeOverrides(
  a: Map<string, ScopeOverride>,
  b: Map<string, ScopeOverride>,
): boolean {
  if (a.size !== b.size) return false;
  for (const [category, override] of a) {
    const other = b.get(category);
    if (
      !other ||
      other.scopeInclude !== override.scopeInclude ||
      other.scopeExempt !== override.scopeExempt
    ) {
      return false;
    }
  }
  return true;
}

function scopeSummaryText(customizedScopeCount: number): string {
  return customizedScopeCount > 0
    ? `Recommended scopes (${customizedScopeCount} customized)`
    : "Recommended scopes";
}

// Combine two include expressions: a message must satisfy both.
function intersectScopeExprs(a: string, b: string): string {
  const left = a.trim();
  const right = b.trim();
  if (left === "") return right;
  if (right === "") return left;
  return `(${left}) && (${right})`;
}

// Combine two exempt expressions: either one takes the message out.
function unionScopeExprs(a: string, b: string): string {
  const left = a.trim();
  const right = b.trim();
  if (left === "") return right;
  if (right === "") return left;
  return `(${left}) || (${right})`;
}

function detectionScopesPayload(
  selectedCategories: Set<RuleCategory>,
  overrides: Map<string, ScopeOverride>,
): RiskDetectionScope[] {
  return [...overrides]
    .filter(([category]) => selectedCategories.has(category as RuleCategory))
    .map(([category, override]) => ({
      category,
      ...(override.scopeInclude.trim()
        ? { scopeInclude: override.scopeInclude.trim() }
        : {}),
      ...(override.scopeExempt.trim()
        ? { scopeExempt: override.scopeExempt.trim() }
        : {}),
    }));
}

// ── Action section (flag vs block) ───────────────────────────────────────────

// Shared Action step — identical for prompt and standard: flag/block picker,
// audience, and the block-time custom message.
// Severity assigns a CVSS-style score (0.1–10, default 5) to the policy.
// Findings inherit it at read time to render a severity badge. Pure metadata —
// changing it never re-scans or regenerates findings.
const SEVERITY_MIN = 0.1;
const SEVERITY_MAX = 10;

function SeveritySection({
  score,
  setScore,
}: {
  score: number;
  setScore: React.Dispatch<React.SetStateAction<number>>;
}): JSX.Element {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label className="text-sm font-medium">Severity</Label>
        <Stack direction="horizontal" gap={2} align="center">
          <Type small mono>
            {score.toFixed(1)}
          </Type>
          <SeverityBadge score={score} />
        </Stack>
      </div>
      <Type small muted>
        Rate how severe this policy's findings are, from {SEVERITY_MIN} to{" "}
        {SEVERITY_MAX}. Findings surface this as a severity badge; it does not
        change what the policy detects.
      </Type>
      <div className="pt-3">
        <Slider
          value={score}
          onChange={(v) =>
            setScore(Math.max(SEVERITY_MIN, Math.min(SEVERITY_MAX, v)))
          }
          min={SEVERITY_MIN}
          max={SEVERITY_MAX}
          step={0.1}
          ticks={[SEVERITY_MIN, 4, 7, 9, SEVERITY_MAX]}
        />
      </div>
    </div>
  );
}

// ── Sensitivity step (Presidio match-confidence threshold) ───────────────────
// The minimum confidence a Presidio PII match must clear to be flagged. Applies
// to every Presidio-backed detector in a standard policy and is only persisted
// while at least one such category is active (see `presidioActive` in the
// editor). Non-Presidio policies don't carry a stray threshold.
const PRESIDIO_THRESHOLD_MIN = 0;
const PRESIDIO_THRESHOLD_MAX = 1;
const PRESIDIO_THRESHOLD_STEP = 0.05;
const PRESIDIO_THRESHOLD_TICKS = [0, 0.25, 0.5, 0.75, 1];
const DEFAULT_PRESIDIO_THRESHOLD = 0.5;
const EMPTY_SHADOW_MCP_URLS: ReadonlySet<string> = new Set<string>();

function SensitivityStep({
  active,
  threshold,
  setThreshold,
}: {
  active: boolean;
  threshold: number;
  setThreshold: React.Dispatch<React.SetStateAction<number>>;
}): JSX.Element {
  return (
    <Card>
      <SectionHeader description="Tune the confidence a match must clear before it's flagged." />
      {active ? (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Label className="text-sm font-medium">Detection sensitivity</Label>
            <Type small mono>
              {threshold.toFixed(2)}
              {threshold === DEFAULT_PRESIDIO_THRESHOLD ? " · default" : ""}
            </Type>
          </div>
          <Type small muted>
            Minimum confidence a match must clear to be flagged, from{" "}
            {PRESIDIO_THRESHOLD_MIN} to {PRESIDIO_THRESHOLD_MAX}. Higher means
            fewer false positives but may miss borderline matches. Applies to
            all Presidio-backed detectors in this policy.
          </Type>
          <div className="pt-3">
            <Slider
              value={threshold}
              onChange={(v) =>
                setThreshold(
                  Math.max(
                    PRESIDIO_THRESHOLD_MIN,
                    Math.min(PRESIDIO_THRESHOLD_MAX, Math.round(v * 20) / 20),
                  ),
                )
              }
              min={PRESIDIO_THRESHOLD_MIN}
              max={PRESIDIO_THRESHOLD_MAX}
              step={PRESIDIO_THRESHOLD_STEP}
              ticks={PRESIDIO_THRESHOLD_TICKS}
            />
          </div>
        </div>
      ) : (
        <Type small muted>
          Sensitivity applies to confidence-scored detectors. Turn on a
          PII-style category (Financial, PII, Government IDs, Healthcare, or
          Off-Policy) in the Detect step to adjust it.
        </Type>
      )}
    </Card>
  );
}

function ActionStep({
  action,
  setAction,
  audienceType,
  setAudienceType,
  audiencePrincipalUrns,
  setAudiencePrincipalUrns,
  userMessage,
  setUserMessage,
  score,
  setScore,
  flagOnlySelected = false,
  shadowMCPAllowedServers,
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
  score: number;
  setScore: React.Dispatch<React.SetStateAction<number>>;
  flagOnlySelected?: boolean;
  shadowMCPAllowedServers?: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-4">
      <Card>
        <SeveritySection score={score} setScore={setScore} />
      </Card>
      <Card>
        <SectionHeader description="Choose how the policy responds when it fires, and who it applies to." />
        <Stack gap={5}>
          <ActionPicker
            formAction={action}
            setFormAction={setAction}
            flagOnlySelected={flagOnlySelected}
          />
          {shadowMCPAllowedServers}
          <PolicyAudiencePicker
            formAudienceType={audienceType}
            setFormAudienceType={setAudienceType}
            selectedAudiencePrincipalUrns={audiencePrincipalUrns}
            setSelectedAudiencePrincipalUrns={setAudiencePrincipalUrns}
          />
          {action !== "flag" && (
            <div className="space-y-2">
              <Label className="text-sm font-medium">
                {action === "warn" ? "Warning message" : "Custom Message"}
              </Label>
              <p className="text-muted-foreground text-xs">
                {action === "warn"
                  ? "Shown to the user when this policy warns on a tool call or prompt. Supports %{match}, %{entity}, %{policy}, and %{rule} placeholders, substituted at warn time. Leave blank to use the default message."
                  : "Shown to the user when this policy blocks a tool call or prompt. Leave blank to use the default message."}
              </p>
              <TextArea
                value={userMessage}
                onChange={setUserMessage}
                placeholder={
                  action === "warn"
                    ? "e.g. %{match} looks sensitive. Acknowledge to proceed."
                    : "e.g. This action was blocked by your organization's security policy. Contact your admin for help."
                }
                rows={3}
              />
            </div>
          )}
        </Stack>
      </Card>
    </div>
  );
}

// ── Evaluate step: prompt-tuning workbench ────────────────────────────────────

type EvalVerdict = "correct" | "false_positive" | "missed";
type JudgeAgreement = "agree" | "disagree";
type EvalMatchFilter = "all" | "flagged" | "clean";

type Guardrail = {
  prompt: string;
  model: string;
  temperature: number;
  failOpen: boolean;
  messageTypes: string[];
  scopeInclude: string;
  scopeExempt: string;
};

const EVAL_SESSION_LIMIT = 8;

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
      scopeInclude: guardrail.scopeInclude.trim() || undefined,
      scopeExempt: guardrail.scopeExempt.trim() || undefined,
    },
  };
}

function evalQueryKey(guardrail: Guardrail, chatId: string) {
  return [
    "@gram/client",
    "evals",
    "evaluate",
    evalRequestBody(guardrail, chatId),
  ] as const;
}

function isFailOpenCleanEval(result: PromptGuardrailEvalResult): boolean {
  return (
    !result.flagged &&
    result.verdicts.length > 0 &&
    result.verdicts.every((v) => v.totalTokens === 0)
  );
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

  // Key by POST body; the generated hook does not.
  return useQuery({
    queryKey: evalQueryKey(guardrail, chatId),
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
    scopeInclude: guardrail.scopeInclude,
    scopeExempt: guardrail.scopeExempt,
  });
}

const JUDGE_QUERY_OPTIONS = {
  // Cache completed judge calls by guardrail and chat.
  staleTime: Infinity,
  gcTime: 5 * 60 * 1000,
  refetchOnWindowFocus: false,
} as const;

function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

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
  reviewVerdictFilter,
  setReviewVerdictFilter,
}: {
  guardrail: Guardrail;
  onPromptChange: (v: string) => void;
  verdicts: Map<string, EvalVerdict>;
  setVerdict: (chatId: string, verdict: EvalVerdict) => void;
  reviewVerdictFilter: EvalVerdict | null;
  setReviewVerdictFilter: (verdict: EvalVerdict | null) => void;
}): JSX.Element {
  // Debounce guardrail edits before judging rows.
  const judgeGuardrail = useDebounced(guardrail, 600);
  const guardrailKey = useMemo(() => guardrailEvalKey(guardrail), [guardrail]);
  const judgeGuardrailKey = useMemo(
    () => guardrailEvalKey(judgeGuardrail),
    [judgeGuardrail],
  );
  const accumulatedEvalIdsRef = useRef<{
    key: string;
    ids: Set<string>;
  }>({ key: judgeGuardrailKey, ids: new Set() });
  const poisonRetryAfterRef = useRef(new Map<string, number>());
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const activeQuery = deferredQuery.trim();
  const chatsQuery = useListChats(
    {
      search: activeQuery || undefined,
      limit: EVAL_SESSION_LIMIT,
      sortBy: SortBy.LastMessageTimestamp,
      sortOrder: SortOrder.Desc,
    },
    undefined,
    {
      placeholderData: keepPreviousData,
    },
  );
  const rawChats = chatsQuery.data?.chats;
  const chats = useMemo(() => rawChats ?? [], [rawChats]);
  const visibleChatIds = useMemo(() => chats.map((chat) => chat.id), [chats]);
  const reviewSetChatIds = useMemo(
    () => Array.from(verdicts.keys()).sort(),
    [verdicts],
  );
  const evalChatIds = useMemo(() => {
    const current = accumulatedEvalIdsRef.current;
    if (current.key !== judgeGuardrailKey) {
      current.key = judgeGuardrailKey;
      current.ids = new Set();
    }
    for (const chatId of visibleChatIds) current.ids.add(chatId);
    for (const chatId of reviewSetChatIds) current.ids.add(chatId);
    return Array.from(current.ids).sort();
  }, [judgeGuardrailKey, reviewSetChatIds, visibleChatIds]);
  const hasJudgeGuardrail = judgeGuardrail.prompt.trim().length > 0;
  const client = useGramContext();
  const queryClient = useQueryClient();
  const evalQueries = useQueries({
    queries: evalChatIds.map((chatId) => {
      const request = evalRequestBody(judgeGuardrail, chatId);
      return {
        queryKey: evalQueryKey(judgeGuardrail, chatId),
        queryFn: ({ signal }: { signal: AbortSignal }) =>
          unwrapAsync(
            riskEvalsEvaluate(client, request, undefined, { signal }),
          ),
        enabled: hasJudgeGuardrail && evalChatIds.length > 0,
        ...JUDGE_QUERY_OPTIONS,
      };
    }),
  });
  const evalByChatId = useMemo(() => {
    const m = new Map<string, PromptGuardrailEvalResult>();
    for (const [index, chatId] of evalChatIds.entries()) {
      const query = evalQueries[index];
      if (
        query?.status === "success" &&
        query.fetchStatus !== "fetching" &&
        query.data?.chatId === chatId &&
        !isFailOpenCleanEval(query.data)
      ) {
        m.set(chatId, query.data);
      }
    }
    return m;
  }, [evalChatIds, evalQueries]);
  useEffect(() => {
    const now = Date.now();
    for (const [index, chatId] of evalChatIds.entries()) {
      const query = evalQueries[index];
      const queryKey = evalQueryKey(judgeGuardrail, chatId);
      if (
        query?.status === "success" &&
        query.fetchStatus !== "fetching" &&
        query.data?.chatId === chatId &&
        isFailOpenCleanEval(query.data)
      ) {
        const retryKey = `${judgeGuardrailKey}:${chatId}`;
        const retryAfter = poisonRetryAfterRef.current.get(retryKey) ?? 0;
        if (now >= retryAfter) {
          poisonRetryAfterRef.current.set(retryKey, now + 5000);
          void queryClient.invalidateQueries({ queryKey, exact: true });
        }
      }
    }
  }, [
    evalChatIds,
    evalQueries,
    judgeGuardrail,
    judgeGuardrailKey,
    queryClient,
  ]);
  const judgingChatIds = useMemo(() => {
    if (!hasJudgeGuardrail) return new Set<string>();
    return new Set(evalChatIds.filter((chatId) => !evalByChatId.has(chatId)));
  }, [evalByChatId, evalChatIds, hasJudgeGuardrail]);
  const reviewSetJudgingCount = useMemo(
    () =>
      reviewSetChatIds.filter((chatId) => judgingChatIds.has(chatId)).length,
    [judgingChatIds, reviewSetChatIds],
  );
  const evalJudging = judgingChatIds.size > 0;
  useEffect(() => {
    if (!reviewVerdictFilter) return;
    for (const current of verdicts.values()) {
      if (current === reviewVerdictFilter) return;
    }
    setReviewVerdictFilter(null);
  }, [reviewVerdictFilter, setReviewVerdictFilter, verdicts]);

  return (
    <div className="grid gap-6 @3xl:grid-cols-2">
      <Stack gap={4}>
        <GuardrailCard
          prompt={guardrail.prompt}
          onPromptChange={onPromptChange}
          rows={10}
        />
        <ReviewScorecard
          verdicts={verdicts}
          evalsByChatId={evalByChatId}
          judgingCount={reviewSetJudgingCount}
          activeVerdict={reviewVerdictFilter}
          onVerdictSelect={(next) =>
            setReviewVerdictFilter(reviewVerdictFilter === next ? null : next)
          }
        />
      </Stack>
      <SessionReview
        guardrail={judgeGuardrail}
        debouncePending={guardrailKey !== judgeGuardrailKey}
        evalJudging={evalJudging}
        evalByChatId={evalByChatId}
        judgingChatIds={judgingChatIds}
        query={query}
        setQuery={setQuery}
        activeQuery={activeQuery}
        chats={chats}
        chatsLoading={chatsQuery.isLoading}
        verdicts={verdicts}
        reviewVerdictFilter={reviewVerdictFilter}
        onClearReviewVerdictFilter={() => setReviewVerdictFilter(null)}
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
  customizedScopeCount,
  action,
  score,
  audienceType,
  audiencePrincipalCount,
  verdicts,
  activeVerdict,
  onVerdictSelect,
}: {
  prompt: string;
  model: string;
  temperature: number;
  failOpen: boolean;
  customizedScopeCount: number;
  action: PolicyAction;
  score: number;
  audienceType: "everyone" | "targeted";
  audiencePrincipalCount: number;
  verdicts: Map<string, EvalVerdict>;
  activeVerdict: EvalVerdict | null;
  onVerdictSelect: (verdict: EvalVerdict) => void;
}): JSX.Element {
  const scopeText = scopeSummaryText(customizedScopeCount);
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
          <SummaryRow label="Action">
            <Badge variant={action === "flag" ? "neutral" : "warning"}>
              {action === "block"
                ? "Block"
                : action === "warn"
                  ? "Warn"
                  : "Flag"}
            </Badge>
          </SummaryRow>
          <SummaryRow label="Severity">
            <SeverityBadge score={score} />
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
      <ReviewScorecard
        verdicts={verdicts}
        activeVerdict={activeVerdict}
        onVerdictSelect={onVerdictSelect}
      />
    </Stack>
  );
}

function ReviewScorecard({
  verdicts,
  evalsByChatId = null,
  judgingCount = 0,
  activeVerdict,
  onVerdictSelect,
}: {
  verdicts: Map<string, EvalVerdict>;
  evalsByChatId?: Map<string, PromptGuardrailEvalResult> | null;
  judgingCount?: number;
  activeVerdict?: EvalVerdict | null;
  onVerdictSelect?: (verdict: EvalVerdict) => void;
}): JSX.Element {
  const reviewed = verdicts.size;
  const judged = evalsByChatId?.size ?? reviewed;
  let correct = 0;
  let falsePositive = 0;
  let missed = 0;
  if (evalsByChatId) {
    for (const [chatId, verdict] of verdicts) {
      const result = evalsByChatId.get(chatId);
      if (!result) continue;
      const agreement = agreementForVerdict(result.flagged, verdict);
      if (agreement === "agree") correct += 1;
      else if (result.flagged && verdict === "false_positive") {
        falsePositive += 1;
      } else if (!result.flagged && verdict === "missed") {
        missed += 1;
      }
    }
  } else {
    for (const v of verdicts.values()) {
      if (v === "correct") correct += 1;
      else if (v === "false_positive") falsePositive += 1;
      else if (v === "missed") missed += 1;
    }
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
              {correct}/{judged}
            </Type>
            <Type small muted>
              {judgingCount > 0
                ? `${judgingCount} judging, ${reviewed} in review set`
                : `match your judgment across ${reviewed} reviewed ${
                    reviewed === 1 ? "session" : "sessions"
                  }`}
            </Type>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <ScoreStat
              label="Correct"
              value={correct}
              verdict="correct"
              active={activeVerdict === "correct"}
              onSelect={onVerdictSelect}
            />
            <ScoreStat
              label="False positives"
              value={falsePositive}
              verdict="false_positive"
              hint="tighten"
              warn={falsePositive > 0}
              active={activeVerdict === "false_positive"}
              onSelect={onVerdictSelect}
            />
            <ScoreStat
              label="Missed"
              value={missed}
              verdict="missed"
              hint="broaden"
              warn={missed > 0}
              active={activeVerdict === "missed"}
              onSelect={onVerdictSelect}
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
  verdict,
  hint,
  warn,
  active,
  onSelect,
}: {
  label: string;
  value: number;
  verdict: EvalVerdict;
  hint?: string;
  warn?: boolean;
  active?: boolean;
  onSelect?: (verdict: EvalVerdict) => void;
}): JSX.Element {
  const clickable = value > 0 && onSelect;
  const content = (
    <>
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
    </>
  );

  if (clickable) {
    return (
      <button
        type="button"
        onClick={() => onSelect(verdict)}
        aria-pressed={active}
        className={cn(
          "rounded-lg border p-3 text-left transition-colors",
          active
            ? "border-foreground/40 bg-muted/70"
            : "hover:bg-muted/40 hover:border-foreground/30",
        )}
      >
        {content}
      </button>
    );
  }

  return <div className="rounded-lg border p-3">{content}</div>;
}

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
  if (filter === "all") return false;
  if (flagged === undefined) return true;
  return filter === "flagged" ? !flagged : flagged;
}

function sessionUserLabel(chat: {
  externalUserId?: string | undefined;
  userId?: string | undefined;
}): string {
  return chat.externalUserId || chat.userId || "Unknown user";
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

function reviewedChatIdsForVerdict(
  verdicts: Map<string, EvalVerdict>,
  verdict: EvalVerdict | null,
): string[] {
  if (!verdict) return [];
  const ids: string[] = [];
  for (const [chatId, current] of verdicts) {
    if (current === verdict) ids.push(chatId);
  }
  return ids;
}

function ReviewedSessionRows({
  chatIds,
  verdict,
  evalsByChatId,
  judgingChatIds,
  activeChatId,
  onClear,
  onSelect,
}: {
  chatIds: string[];
  verdict: EvalVerdict;
  evalsByChatId: Map<string, PromptGuardrailEvalResult>;
  judgingChatIds: Set<string>;
  activeChatId: string | null;
  onClear: () => void;
  onSelect: (chatId: string) => void;
}): JSX.Element {
  return (
    <div>
      <div className="bg-muted/30 flex items-center justify-between gap-3 border-b px-3 py-2">
        <div className="min-w-0">
          <Type small className="font-medium">
            {evalVerdictLabel(verdict)}
          </Type>
          <Type small muted>
            {chatIds.length} reviewed{" "}
            {chatIds.length === 1 ? "session" : "sessions"}
          </Type>
        </div>
        <Button variant="secondary" size="sm" onClick={onClear}>
          <Button.Text>Clear</Button.Text>
        </Button>
      </div>
      {chatIds.length === 0 ? (
        <Type small muted className="block px-3 py-6 text-center">
          No reviewed sessions for this verdict.
        </Type>
      ) : (
        chatIds.map((chatId, i) => (
          <ReviewedSessionRow
            key={chatId}
            chatId={chatId}
            verdict={verdict}
            evalResult={evalsByChatId.get(chatId)}
            judging={judgingChatIds.has(chatId)}
            active={chatId === activeChatId}
            first={i === 0}
            onSelect={() => onSelect(chatId)}
          />
        ))
      )}
    </div>
  );
}

function ReviewedSessionRow({
  chatId,
  verdict,
  evalResult,
  judging,
  active,
  first,
  onSelect,
}: {
  chatId: string;
  verdict: EvalVerdict;
  evalResult: PromptGuardrailEvalResult | undefined;
  judging: boolean;
  active: boolean;
  first: boolean;
  onSelect: () => void;
}): JSX.Element {
  const chatQuery = useLoadChat(
    { id: chatId, limit: 1, fromStart: true },
    undefined,
    {
      staleTime: 5 * 60 * 1000,
      throwOnError: false,
    },
  );
  const chat = chatQuery.data;
  const title = chat?.title || "Untitled session";
  const userLabel = chat ? sessionUserLabel(chat) : "Resolving session…";

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors",
        !first && "border-t",
        active ? "bg-muted/60" : "hover:bg-muted/30",
      )}
    >
      <div className="min-w-0 flex-1">
        <Type small className="truncate font-medium">
          {title}
        </Type>
        <Type small muted className="flex min-w-0 items-center gap-1 truncate">
          <span className="truncate">{userLabel}</span>
          {chat ? (
            <>
              <span>·</span>
              <span>{formatRelative(chat.lastMessageTimestamp)}</span>
            </>
          ) : null}
          {chatQuery.isError ? (
            <>
              <span>·</span>
              <span>metadata unavailable</span>
            </>
          ) : null}
        </Type>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        <SessionMatchBadge
          enabled
          judging={judging}
          flagged={evalResult?.flagged}
        />
        <Badge variant={verdict === "missed" ? "warning" : "neutral"}>
          {evalVerdictLabel(verdict)}
        </Badge>
      </div>
    </button>
  );
}

function SessionReview({
  guardrail,
  debouncePending,
  evalJudging,
  evalByChatId,
  judgingChatIds,
  query,
  setQuery,
  activeQuery,
  chats,
  chatsLoading,
  verdicts,
  reviewVerdictFilter,
  onClearReviewVerdictFilter,
  onVerdict,
}: {
  guardrail: Guardrail;
  debouncePending: boolean;
  evalJudging: boolean;
  evalByChatId: Map<string, PromptGuardrailEvalResult>;
  judgingChatIds: Set<string>;
  query: string;
  setQuery: (query: string) => void;
  activeQuery: string;
  chats: ChatOverview[];
  chatsLoading: boolean;
  verdicts: Map<string, EvalVerdict>;
  reviewVerdictFilter: EvalVerdict | null;
  onClearReviewVerdictFilter: () => void;
  onVerdict: (chatId: string, verdict: EvalVerdict) => void;
}): JSX.Element {
  const [filter, setFilter] = useState<EvalMatchFilter>("all");
  const [selectedIdState, setSelectedIdState] = useState<string | null>(null);
  const reviewedChatIds = useMemo(
    () => reviewedChatIdsForVerdict(verdicts, reviewVerdictFilter),
    [verdicts, reviewVerdictFilter],
  );
  const hasGuardrail = guardrail.prompt.trim().length > 0;
  const visibleChats = useMemo(
    () =>
      chats.filter(
        (chat) =>
          !rowHiddenByFilter(filter, evalByChatId.get(chat.id)?.flagged),
      ),
    [chats, evalByChatId, filter],
  );

  // Do not auto-open a row while search/filter results change.
  const selectedId =
    selectedIdState &&
    (chats.some((c) => c.id === selectedIdState) ||
      reviewedChatIds.includes(selectedIdState))
      ? selectedIdState
      : null;
  const selectedChat = chats.find((c) => c.id === selectedId) ?? null;
  const reevaluating = hasGuardrail && (debouncePending || evalJudging);

  return (
    <Card className="flex flex-col">
      <SectionHeader description="Search by title or user, review how this guardrail judges the transcript, then mark the verdict." />
      <div className="flex min-h-0 flex-1 flex-col gap-4">
        {/* Search */}
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
        <div className="min-h-0 flex-1 overflow-auto rounded-lg border">
          {reviewVerdictFilter ? (
            <ReviewedSessionRows
              chatIds={reviewedChatIds}
              verdict={reviewVerdictFilter}
              evalsByChatId={evalByChatId}
              judgingChatIds={judgingChatIds}
              activeChatId={selectedId}
              onClear={onClearReviewVerdictFilter}
              onSelect={setSelectedIdState}
            />
          ) : chatsLoading ? (
            <Type small muted className="block px-3 py-6 text-center">
              Loading sessions…
            </Type>
          ) : chats.length === 0 ? (
            <Type small muted className="block px-3 py-6 text-center">
              No sessions match your search.
            </Type>
          ) : visibleChats.length === 0 ? (
            <Type small muted className="block px-3 py-6 text-center">
              No sessions match this judge filter.
            </Type>
          ) : (
            visibleChats.map((chat, i) => (
              <SessionRow
                key={chat.id}
                chat={chat}
                active={chat.id === selectedId}
                first={i === 0}
                evalEnabled={hasGuardrail}
                flagged={evalByChatId.get(chat.id)?.flagged}
                judging={judgingChatIds.has(chat.id)}
                reviewVerdict={verdicts.get(chat.id) ?? null}
                searchQuery={activeQuery}
                onSelect={() => setSelectedIdState(chat.id)}
              />
            ))
          )}
        </div>
      </div>
      <EvalSessionTranscriptSheet
        chatId={selectedId}
        chat={selectedChat}
        guardrail={guardrail}
        verdict={selectedId ? (verdicts.get(selectedId) ?? null) : null}
        reviewDisabled={debouncePending}
        onClose={() => setSelectedIdState(null)}
        onVerdict={(v) => {
          if (selectedId) onVerdict(selectedId, v);
        }}
      />
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
  active,
  first,
  evalEnabled,
  flagged,
  judging,
  reviewVerdict,
  searchQuery,
  onSelect,
}: {
  chat: ChatOverview;
  active: boolean;
  first: boolean;
  evalEnabled: boolean;
  flagged: boolean | undefined;
  judging: boolean;
  reviewVerdict: EvalVerdict | null;
  searchQuery: string;
  onSelect: () => void;
}): JSX.Element {
  const title = chat.title || "Untitled session";
  const userLabel = sessionUserLabel(chat);

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors",
        !first && "border-t",
        active ? "bg-muted/60" : "hover:bg-muted/30",
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
          <span>{formatRelative(chat.lastMessageTimestamp)}</span>
        </Type>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        <SessionMatchBadge
          enabled={evalEnabled}
          judging={judging || flagged === undefined}
          flagged={flagged}
        />
        {reviewVerdict ? (
          <Badge variant="neutral">{evalVerdictLabel(reviewVerdict)}</Badge>
        ) : null}
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

function EvalSessionTranscriptSheet({
  chatId,
  chat,
  guardrail,
  verdict,
  reviewDisabled,
  onClose,
  onVerdict,
}: {
  chatId: string | null;
  chat: ChatOverview | null;
  guardrail: Guardrail;
  verdict: EvalVerdict | null;
  reviewDisabled: boolean;
  onClose: () => void;
  onVerdict: (v: EvalVerdict) => void;
}): JSX.Element {
  return (
    <Sheet
      open={Boolean(chatId)}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        className="flex h-full w-[min(760px,calc(100vw-2rem))] flex-col gap-0 p-0 sm:max-w-[760px]"
        showCloseButton={false}
      >
        {chatId && (
          <SessionTranscript
            chatId={chatId}
            chat={chat}
            guardrail={guardrail}
            verdict={verdict}
            reviewDisabled={reviewDisabled}
            onClose={onClose}
            onVerdict={onVerdict}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function EvalJudgeVerdictBlock({
  verdict,
}: {
  verdict: PromptGuardrailMessageVerdict;
}): JSX.Element {
  return (
    <div className="border-warning bg-warning/10 rounded-sm border-l-[3px] px-3 py-2.5">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-xs font-semibold">
          <TriangleAlert className="text-warning size-4 shrink-0" />
          <span>LLM Judge</span>
          <Badge variant="warning" background>
            Flagged
          </Badge>
        </div>
        <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
          {formatEvalConfidence(verdict.confidence)} ·{" "}
          {formatEvalLatency(verdict.latencyMs)}
        </span>
      </div>
      <div className="text-foreground mt-1 text-xs leading-relaxed">
        {verdict.rationale || "Flagged by the guardrail"}
      </div>
    </div>
  );
}

function JudgeSessionBanner({
  evalResult,
  fetching,
}: {
  evalResult: PromptGuardrailEvalResult | undefined;
  fetching: boolean;
}): JSX.Element {
  if (fetching && !evalResult) {
    return (
      <div className="border-border bg-muted/30 flex items-start gap-3 border-b px-4 py-3">
        <Loader2 className="text-muted-foreground mt-0.5 size-4 shrink-0 animate-spin" />
        <div className="min-w-0">
          <Type small className="font-medium">
            Judge is evaluating this session
          </Type>
          <Type small muted>
            Verdicts will appear against matching messages when the run
            finishes.
          </Type>
        </div>
      </div>
    );
  }

  if (!evalResult) {
    return (
      <div className="border-border bg-muted/20 flex items-start gap-3 border-b px-4 py-3">
        <Shield className="text-muted-foreground mt-0.5 size-4 shrink-0" />
        <div className="min-w-0">
          <Type small className="font-medium">
            Opened for judge evaluation
          </Type>
          <Type small muted>
            The session has not returned a judge result yet.
          </Type>
        </div>
      </div>
    );
  }

  const matchedCount = evalResult.verdicts.filter((v) => v.matched).length;
  const judgedLabel = `${evalResult.judgedCount} judged ${
    evalResult.judgedCount === 1 ? "message" : "messages"
  }`;
  const detail = `${matchedCount} ${
    matchedCount === 1 ? "message" : "messages"
  } matched`;

  if (evalResult.flagged) {
    return (
      <div className="border-warning/40 bg-warning/15 flex items-start gap-3 border-b px-4 py-3">
        <TriangleAlert className="text-warning mt-0.5 size-4 shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Type small className="font-semibold">
              Judge flagged this session
            </Type>
            <Badge variant="warning" background>
              Flagged
            </Badge>
          </div>
          <Type small muted>
            {detail} across {judgedLabel}. Matching messages are highlighted
            below.
          </Type>
        </div>
      </div>
    );
  }

  return (
    <div className="border-success/35 bg-success/10 flex items-start gap-3 border-b px-4 py-3">
      <Check className="text-success mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <Type small className="font-semibold">
            Judge found this session clean
          </Type>
          <Badge variant="neutral">Clean</Badge>
        </div>
        <Type small muted>
          No messages matched across {judgedLabel}.
        </Type>
      </div>
    </div>
  );
}

function SessionTranscript({
  chatId,
  chat,
  guardrail,
  verdict,
  reviewDisabled,
  onClose,
  onVerdict,
}: {
  chatId: string;
  chat: ChatOverview | null;
  guardrail: Guardrail;
  verdict: EvalVerdict | null;
  reviewDisabled: boolean;
  onClose: () => void;
  onVerdict: (v: EvalVerdict) => void;
}): JSX.Element {
  const transcript = useChatTranscript(chatId, true);
  const headerChat = chat ?? transcript.chat ?? null;
  const judge = usePromptGuardrailEval(
    guardrail,
    chatId,
    guardrail.prompt.trim().length > 0,
  );
  const evalResult = judge.data;
  const judgeFetching = judge.isFetching;

  const verdictByMessage = useMemo(() => {
    const m = new Map<string, PromptGuardrailMessageVerdict>();
    for (const v of evalResult?.verdicts ?? []) m.set(v.messageId, v);
    return m;
  }, [evalResult]);

  const rows = useMemo(
    () => buildTranscript(transcript.messages),
    [transcript.messages],
  );
  const displayItems = useMemo(
    () =>
      buildDisplayItems({
        rows,
        hasMoreBefore: transcript.hasMoreBefore,
        hasMoreAfter: transcript.hasMoreAfter,
      }),
    [rows, transcript.hasMoreBefore, transcript.hasMoreAfter],
  );
  const pagination = useMemo<TranscriptPagination>(
    () => ({
      hasMoreBefore: transcript.hasMoreBefore,
      hasMoreAfter: transcript.hasMoreAfter,
      onLoadOlder: transcript.fetchOlder,
      onLoadNewer: transcript.fetchNewer,
      // The from-start transcript's only missing range is the tail, so the
      // break button drains the rest of the conversation.
      onLoadAllOlder: transcript.fetchOlder,
      onLoadAllNewer: transcript.loadRest,
      isFetchingOlder: transcript.isFetchingOlder,
      isFetchingNewer: transcript.isFetchingNewer || transcript.isLoadingRest,
      initialScrollIndex: null,
      scrollToFinding: false,
    }),
    [
      transcript.hasMoreBefore,
      transcript.hasMoreAfter,
      transcript.fetchOlder,
      transcript.fetchNewer,
      transcript.loadRest,
      transcript.isFetchingOlder,
      transcript.isFetchingNewer,
      transcript.isLoadingRest,
    ],
  );
  const rowCtx = useMemo<RowContext>(
    () => ({
      dimNonRisk: false,
      userLabel: headerChat ? sessionUserLabel(headerChat) : undefined,
      rowDecoration: (messageIds) => {
        const matched = messageIds
          .map((id) => verdictByMessage.get(id))
          .find((v) => v?.matched);
        if (!matched) return null;
        return {
          tone: "warning",
          footer: <EvalJudgeVerdictBlock verdict={matched} />,
        };
      },
    }),
    [headerChat, verdictByMessage],
  );
  const judgeFlagged = evalResult?.flagged;
  const judgeSettled = judgeFlagged !== undefined && !judgeFetching;
  const canReview = judgeSettled && !reviewDisabled;

  return (
    <div className="bg-background flex h-full min-h-0 flex-col">
      <SheetHeader className="border-b px-4 py-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 flex-col gap-1.5">
            <SheetTitle className="truncate text-base">
              {headerChat?.title || "Untitled session"}
            </SheetTitle>
            <SheetDescription asChild>
              <div className="flex flex-col gap-2">
                <div className="text-muted-foreground flex min-w-0 items-center gap-1.5 text-sm">
                  <span className="truncate">
                    {headerChat
                      ? sessionUserLabel(headerChat)
                      : "Resolving session…"}
                  </span>
                  {headerChat ? (
                    <>
                      <span>·</span>
                      <span>
                        {formatRelative(headerChat.lastMessageTimestamp)}
                      </span>
                    </>
                  ) : null}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {headerChat ? (
                    <Badge variant="neutral">
                      {headerChat.numMessages} messages
                    </Badge>
                  ) : null}
                  {headerChat?.source ? (
                    <Badge variant="neutral">{headerChat.source}</Badge>
                  ) : null}
                  {evalResult ? (
                    <Badge variant="neutral">
                      {formatEvalCostLatency(
                        evalResult.totalCostUsd,
                        evalResult.totalLatencyMs,
                      )}
                    </Badge>
                  ) : null}
                  {judgeFetching ? (
                    <Badge variant="neutral">Judging…</Badge>
                  ) : null}
                </div>
              </div>
            </SheetDescription>
          </div>
          <button
            onClick={onClose}
            className="hover:bg-muted rounded-md p-1 transition-colors"
            aria-label="Close panel"
          >
            <X className="size-5" />
          </button>
        </div>
      </SheetHeader>

      <JudgeSessionBanner evalResult={evalResult} fetching={judgeFetching} />

      <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {transcript.isLoading ? (
            <Type small muted className="block p-6 text-center">
              Loading transcript…
            </Type>
          ) : transcript.isError ? (
            <Type small muted className="block p-6 text-center">
              Failed to load transcript.
            </Type>
          ) : transcript.messages.length === 0 ? (
            <Type small muted className="block p-6 text-center">
              This session has no messages.
            </Type>
          ) : (
            <ChatTranscript
              items={displayItems}
              ctx={rowCtx}
              pagination={pagination}
              emptyMessage="This session has no messages."
            />
          )}
        </div>
      </div>

      <SheetFooter className="border-t px-4 py-3">
        <ReviewAgreementControl
          flagged={judgeFlagged}
          verdict={verdict}
          disabled={!canReview}
          onVerdict={onVerdict}
        />
      </SheetFooter>
    </div>
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
    <Stack gap={3} align="center">
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

// ── Standard policy editor (stepped: Detect → Scope → Action → Review) ────────
// Reuses PolicyCenter's detector/scope/action/audience building blocks and its
// payload mapping. `policy === null` means create mode.

export function StandardPolicyEditor({
  policy,
}: {
  policy: RiskPolicy | null;
}): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const queryClient = useQueryClient();
  const { customRules } = useDetectionRulesStore();
  const [initializedInventoryForPolicy, setInitializedInventoryForPolicy] =
    useState<string | null>(null);

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
      disabledRules: new Set(policy.disabledRules ?? []),
      scopeOverrides: scopeOverridesFromPolicy(policy.detectionScopes),
      customRuleIds: new Set(policy.customRuleIds ?? []),
      categories: cats,
      approvedDomains: (policy.approvedEmailDomains ?? []).join(", "),
      audienceType:
        policy.audienceType === "targeted"
          ? ("targeted" as const)
          : ("everyone" as const),
      audiencePrincipalUrns:
        policy.audienceType === "targeted"
          ? new Set(policy.audiencePrincipalUrns ?? [])
          : new Set<string>(),
      score: policy.score ?? 5,
      presidioThreshold:
        policy.presidioScoreThreshold ?? DEFAULT_PRESIDIO_THRESHOLD,
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
  const [scopeOverrides, setScopeOverrides] = useState<
    Map<string, ScopeOverride>
  >(() => scopeOverridesFromPolicy(policy?.detectionScopes));
  const [selectedCustomRuleIds, setSelectedCustomRuleIds] = useState<
    Set<string>
  >(() => new Set(policy?.customRuleIds ?? []));
  const [action, setAction] = useState<PolicyAction>(
    (policy?.action as PolicyAction) ?? "flag",
  );
  const [selectedShadowMCPURLs, setSelectedShadowMCPURLs] = useState<
    Set<string>
  >(() => new Set());
  const [originalShadowMCPURLs, setOriginalShadowMCPURLs] =
    useState<Set<string> | null>(null);
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
  // Approved email domains for the Non-Corporate Accounts category, held as
  // the raw comma-separated input. Only sent when the category is selected.
  const [approvedDomains, setApprovedDomains] = useState(() =>
    (policy?.approvedEmailDomains ?? []).join(", "),
  );
  const [customizeCategory, setCustomizeCategory] =
    useState<RuleCategory | null>(null);
  const [detectionExpanded, setDetectionExpanded] = useState(true);
  const [score, setScore] = useState(policy?.score ?? 5);
  const [presidioThreshold, setPresidioThreshold] = useState<number>(
    policy?.presidioScoreThreshold ?? DEFAULT_PRESIDIO_THRESHOLD,
  );

  // ── Derived state ──
  const targetIsShadowMCPBlock = isBlockingShadowMCPPolicy(
    true,
    [...selectedCategories],
    action,
  );
  const inventoryQuery = useShadowMCPPolicyInventory(
    project.id,
    targetIsShadowMCPBlock,
  );
  const policyID = policy?.id ?? null;
  const editorIdentity = policyID ?? "create";
  const originalHasShadowMCPBlockConfiguration = policy
    ? isShadowMCPBlockConfiguration(policy.sources, policy.action)
    : false;

  useEffect(() => {
    if (
      !targetIsShadowMCPBlock ||
      !inventoryQuery.data ||
      initializedInventoryForPolicy === editorIdentity
    ) {
      return;
    }

    const initialURLs =
      policyID && originalHasShadowMCPBlockConfiguration
        ? initialShadowMCPPolicyURLs(inventoryQuery.data, policyID)
        : new Set<string>();
    setSelectedShadowMCPURLs(new Set(initialURLs));
    setOriginalShadowMCPURLs(new Set(initialURLs));
    setInitializedInventoryForPolicy(editorIdentity);
  }, [
    editorIdentity,
    initializedInventoryForPolicy,
    inventoryQuery.data,
    originalHasShadowMCPBlockConfiguration,
    policyID,
    targetIsShadowMCPBlock,
  ]);

  const flagOnlySelected = [...FLAG_ONLY_CATEGORIES].some((c) =>
    selectedCategories.has(c),
  );
  const presidioActive = PRESIDIO_CATEGORIES.some((c) =>
    selectedCategories.has(c),
  );
  const hasEnabledDetector =
    selectedCustomRuleIds.size > 0 ||
    [...selectedCategories].some(
      (c) =>
        CATEGORY_LEVEL_DETECTORS.has(c) ||
        DETECTION_RULES[c]?.some((r) => !r.hidden && !disabledRules.has(r.id)),
    );
  const audienceMissing =
    audienceType === "targeted" && audiencePrincipalUrns.size === 0;
  const shadowMCPInventoryUnavailable =
    targetIsShadowMCPBlock &&
    (inventoryQuery.isPending ||
      inventoryQuery.isError ||
      inventoryQuery.data === undefined);
  const shadowMCPSelectionInitialized = shadowMCPSelectionIsInitialized(
    targetIsShadowMCPBlock,
    initializedInventoryForPolicy,
    editorIdentity,
  );
  const saveBlocked =
    !hasEnabledDetector ||
    audienceMissing ||
    shadowMCPInventoryUnavailable ||
    !shadowMCPSelectionInitialized;

  const shadowMCPSelectionDirty = shadowMCPSelectionIsDirty(
    targetIsShadowMCPBlock,
    selectedShadowMCPURLs,
    originalShadowMCPURLs,
  );

  const dirty =
    !!orig &&
    (name !== orig.name ||
      action !== orig.action ||
      userMessage !== orig.userMessage ||
      audienceType !== orig.audienceType ||
      !sameSet(disabledRules, orig.disabledRules) ||
      !sameScopeOverrides(scopeOverrides, orig.scopeOverrides) ||
      !sameSet(selectedCustomRuleIds, orig.customRuleIds) ||
      !sameSet(selectedCategories, orig.categories) ||
      approvedDomains !== orig.approvedDomains ||
      score !== orig.score ||
      (presidioActive && presidioThreshold !== orig.presidioThreshold) ||
      !sameSet(audiencePrincipalUrns, orig.audiencePrincipalUrns) ||
      shadowMCPSelectionDirty);

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: (_policy, variables) => {
      const submittedURLs = shadowMCPSelectionBaselineForUpdate(
        variables.request.updateRiskPolicyRequestBody,
      );
      if (submittedURLs !== undefined) {
        setSelectedShadowMCPURLs(new Set(submittedURLs));
        setOriginalShadowMCPURLs(new Set(submittedURLs));
      }
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllShadowMCPInventory(queryClient);
      void invalidateShadowMCPPolicyInventory(queryClient, project.id);
    },
  });
  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
      void invalidateAllShadowMCPInventory(queryClient);
      void invalidateShadowMCPPolicyInventory(queryClient, project.id);
      routes.policyCenter.goTo();
    },
  });
  const saving = updateMutation.isPending || createMutation.isPending;

  // Toggle a whole built-in detector category (clears its per-rule disables).
  // Flag-only categories force the policy action to flag.
  const toggleCategory = (cat: RuleCategory, checked: boolean) => {
    if (
      checked &&
      builtInRuleDisabledReason(cat, selectedCategories) !== undefined
    ) {
      return;
    }

    const rules = DETECTION_RULES[cat].filter((r) => !r.hidden);
    const nextCats = new Set(selectedCategories);
    const nextDisabled = new Set(disabledRules);
    if (checked) nextCats.add(cat);
    else nextCats.delete(cat);
    for (const rule of rules) nextDisabled.delete(rule.id);
    setSelectedCategories(nextCats);
    setDisabledRules(nextDisabled);
    if (checked && FLAG_ONLY_CATEGORIES.has(cat) && action !== "flag") {
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
    // Flag-only sources (destructive_tool, cli_destructive, account_identity)
    // are rejected by the server with action=block, so force flag as a safety
    // net in case the form state drifted.
    const flagOnlyActive = sources.some((s) =>
      FLAG_ONLY_CATEGORIES.has(s as RuleCategory),
    );
    const resolvedAction =
      flagOnlyActive && action !== "flag" ? "flag" : action;
    const principals =
      audienceType === "targeted" ? [...audiencePrincipalUrns] : [];
    const autoName = name.trim() === "";
    // Only send approved domains while the Non-Corporate Accounts category is
    // selected (an empty array clears them); omit otherwise so the server
    // preserves whatever is stored.
    const identityActive = selectedCategories.has("account_identity");
    const approvedEmailDomains = parseApprovedEmailDomains(approvedDomains);
    const detectionScopes = detectionScopesPayload(
      selectedCategories,
      scopeOverrides,
    );
    const shadowMcpAllowedUrls = shadowMCPAllowedURLsForMutation({
      action: resolvedAction,
      selectedCategories,
      selectedURLs: selectedShadowMCPURLs,
      originalPolicy: policy,
    });
    const setupFields =
      shadowMcpAllowedUrls === undefined ? {} : { shadowMcpAllowedUrls };

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
            detectionScopes,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            action: resolvedAction,
            audienceType,
            audiencePrincipalUrns: principals,
            autoName,
            userMessage,
            score,
            // Always send: default when no Presidio category is active, so
            // disabling them resets the stored threshold instead of leaving a
            // stale value that would resurface if Presidio is re-enabled later
            // (update omits preserve prior values server-side).
            presidioScoreThreshold: presidioActive
              ? presidioThreshold
              : DEFAULT_PRESIDIO_THRESHOLD,
            ...setupFields,
            ...(identityActive ? { approvedEmailDomains } : {}),
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
            ...(detectionScopes.length > 0 ? { detectionScopes } : {}),
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            action: resolvedAction,
            audienceType,
            audiencePrincipalUrns: principals,
            autoName,
            ...(userMessage.trim() ? { userMessage } : {}),
            score,
            ...(presidioActive
              ? { presidioScoreThreshold: presidioThreshold }
              : {}),
            ...setupFields,
            ...(identityActive ? { approvedEmailDomains } : {}),
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
                      disabledReason={builtInRuleDisabledReason(
                        cat,
                        selectedCategories,
                      )}
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
          <SensitivityStep
            active={presidioActive}
            threshold={presidioThreshold}
            setThreshold={setPresidioThreshold}
          />
        )}

        {step === 2 && (
          <ScopeStep
            description="Apply everywhere, or narrow the scope to reduce noise and cost."
            selectedCategories={selectedCategories}
            scopeOverrides={scopeOverrides}
            setScopeOverrides={setScopeOverrides}
            legacyPolicy={policy}
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
            score={score}
            setScore={setScore}
            flagOnlySelected={flagOnlySelected}
            shadowMCPAllowedServers={
              targetIsShadowMCPBlock ? (
                <ShadowMCPPolicyServerSelector
                  servers={inventoryQuery.data ?? []}
                  originalURLs={originalShadowMCPURLs ?? EMPTY_SHADOW_MCP_URLS}
                  selectedURLs={selectedShadowMCPURLs}
                  onSelectionChange={setSelectedShadowMCPURLs}
                  isLoading={inventoryQuery.isPending}
                  error={inventoryQuery.error}
                  onRetry={() => void inventoryQuery.refetch()}
                />
              ) : undefined
            }
          />
        )}

        {step === 4 && (
          <StandardReview
            name={name}
            categories={selectedCategories}
            customRuleCount={selectedCustomRuleIds.size}
            customizedScopeCount={scopeOverrides.size}
            action={action}
            score={score}
            presidioActive={presidioActive}
            presidioThreshold={presidioThreshold}
            audienceType={audienceType}
            audiencePrincipalCount={audiencePrincipalUrns.size}
          />
        )}
      </StepperShell>

      {customizeCategory && (
        <CustomizeRulesSheet
          category={customizeCategory}
          selectedCategories={selectedCategories}
          setSelectedCategories={setSelectedCategories}
          disabledRules={disabledRules}
          setDisabledRules={setDisabledRules}
          approvedDomains={approvedDomains}
          setApprovedDomains={setApprovedDomains}
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
  customizedScopeCount,
  action,
  score,
  presidioActive,
  presidioThreshold,
  audienceType,
  audiencePrincipalCount,
}: {
  name: string;
  categories: Set<RuleCategory>;
  customRuleCount: number;
  customizedScopeCount: number;
  action: PolicyAction;
  score: number;
  presidioActive: boolean;
  presidioThreshold: number;
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

  const scopeText = scopeSummaryText(customizedScopeCount);

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
        {presidioActive ? (
          <SummaryRow label="Sensitivity">
            <Type small mono>
              {presidioThreshold.toFixed(2)}
              {presidioThreshold === DEFAULT_PRESIDIO_THRESHOLD
                ? " · default"
                : ""}
            </Type>
          </SummaryRow>
        ) : null}
        <SummaryRow label="Scope">
          <Type small className="text-right">
            {scopeText}
          </Type>
        </SummaryRow>
        <SummaryRow label="Action">
          <Badge variant={action === "flag" ? "neutral" : "warning"}>
            {action === "block" ? "Block" : action === "warn" ? "Warn" : "Flag"}
          </Badge>
        </SummaryRow>
        <SummaryRow label="Severity">
          <SeverityBadge score={score} />
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

function formatEvalConfidence(confidence: number): string {
  return `${Math.round(confidence * 100)}%`;
}

function formatEvalLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "0ms";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
}

function formatEvalCostLatency(costUsd: number, latencyMs: number): string {
  return `${formatUsageCost(costUsd)} eval · ${formatEvalLatency(latencyMs)}`;
}
