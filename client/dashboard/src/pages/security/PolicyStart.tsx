import { Page } from "@/components/page-layout";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { cn } from "@/lib/utils";
import type { RiskPreset } from "@gram/client/models/components/riskpreset.js";
import type { SuggestRiskPolicyResult } from "@gram/client/models/components/suggestriskpolicyresult.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useRiskPresets } from "@gram/client/react-query/riskPresets.js";
import { useRiskSuggestPolicyMutation } from "@gram/client/react-query/riskSuggestPolicy.js";
import { ArrowLeft, ArrowRight, Loader2, Sparkles } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { toast } from "sonner";
import { useQueryState } from "nuqs";
import { ruleCategoryMeta, type DetectorMode } from "./policy-data";
import {
  isShadowAIUseCase,
  presentShadowAIUseCase,
} from "./shadow-ai-use-case";
import {
  useShadowAIPolicySlot,
  type ShadowAIPolicySlot,
} from "./use-shadow-ai-policy-slot";
import {
  actionBadgeVariant,
  draftFromPreset,
  draftFromSuggestion,
  draftKind,
  loadPolicyStartSession,
  matchMCPServers,
  readPolicyStartRestore,
  restoreFromDraft,
  savePolicyStartSession,
  sessionOnlyReason,
  type PolicyDraft,
} from "./policy-draft";
import { policyToCategories } from "./policy-form";
import { SeverityBadge } from "./risk-ui";
import { useDetectorMode } from "./use-detector-mode";

const ACTION_LABEL: Record<string, string> = {
  flag: "Flag",
  warn: "Warn",
  block: "Block",
  quarantine: "Quarantine",
};

const ACTION_LONG_LABEL: Record<string, string> = {
  flag: "Logs it for review",
  warn: "Warns the user and asks them to confirm",
  block: "Blocks the prompt or tool call",
  quarantine: "Blocks and freezes the session for an administrator",
};

const PRESET_ICON: Record<string, IconName> = {
  secrets_and_credentials: "key-round",
  customer_pii_egress: "user",
  destructive_production_actions: "sparkles",
  unapproved_mcp_servers: "shield-off",
  non_corporate_accounts: "user-x",
  prompt_injection: "syringe",
};

const NO_MATCH_RATIONALE =
  "Your description becomes the instruction for a prompt-based guardrail. The policy model judges each in-scope message against it.";

const EXAMPLES = [
  "Block API keys and passwords from being pasted into prompts",
  "Stop agents deleting anything in production",
  "Flag anyone using a personal gmail account",
];

function presetIcon(presetId: string, policyType: string): IconName {
  return (
    PRESET_ICON[presetId] ??
    (policyType === "prompt_based" ? "sparkles" : "shield")
  );
}

/** What turns a draft on, in the detect step's own category names. */
function detectsSummary(
  draft: Pick<PolicyDraft, "policyType" | "sources" | "presidioEntities">,
  mode: DetectorMode,
): string {
  if (draft.policyType === "prompt_based") return "Policy model (LLM judge)";
  const labels = [
    ...policyToCategories(draft.sources, draft.presidioEntities, mode),
  ].map((category) => ruleCategoryMeta(category, mode).label);
  const entities =
    mode === "presidio" && draft.presidioEntities.length > 0
      ? ` (${draft.presidioEntities.length} entities)`
      : "";
  return labels.join(" · ") + entities;
}

function Eyebrow({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return <span className={cn("text-eyebrow", className)}>{children}</span>;
}

function IconTile({
  name,
  size = "md",
}: {
  name: IconName;
  size?: "sm" | "md";
}): JSX.Element {
  return (
    <span
      className={cn(
        "border-border bg-card flex shrink-0 items-center justify-center border",
        size === "md" ? "size-10" : "size-8",
      )}
    >
      <Icon name={name} className="text-muted-foreground size-4" />
    </span>
  );
}

function ActionBadge({ action }: { action: string }): JSX.Element {
  return (
    <Badge variant={actionBadgeVariant(action)}>
      <Badge.Text>{ACTION_LABEL[action] ?? action}</Badge.Text>
    </Badge>
  );
}

function Severity({ score }: { score: number }): JSX.Element {
  return (
    <span className="flex items-center gap-2">
      <SeverityBadge score={score} />
      <span className="text-muted-foreground font-mono text-xs">
        {score.toFixed(1)}
      </span>
    </span>
  );
}

// ── Intent screen ────────────────────────────────────────────────────────────

type ReviewState = {
  suggestion: SuggestRiskPolicyResult;
  overridePresetId: string | null;
};

// Shown when the create page is opened without a `?kind=` hint. The
// administrator picks a use case, or describes the risk and reviews the draft
// it maps to; either way the wizard opens with a draft already filled in.
export function PolicyStartChooser(): JSX.Element {
  const navigate = useNavigate();
  const location = useLocation();
  const telemetry = useTelemetry();
  const gramProject = useProjectSlugForRequests();
  const mode = useDetectorMode();
  const promptPoliciesEnabled =
    telemetry.isFeatureEnabled("gram-prompt-policies") ?? false;
  const [, setKind] = useQueryState("kind");
  // "Change starting point" hands the state over in the route; otherwise
  // pick up a description left behind earlier in this browser session.
  const [restore] = useState(
    () =>
      readPolicyStartRestore(location.state) ??
      loadPolicyStartSession(gramProject),
  );
  const [description, setDescription] = useState(restore?.description ?? "");
  const [review, setReview] = useState<ReviewState | null>(
    restore?.review ?? null,
  );
  useEffect(() => {
    savePolicyStartSession(gramProject, { description, review });
  }, [gramProject, description, review]);
  const { data, isLoading, isError, refetch } = useRiskPresets();
  // The server's "Unapproved MCP servers" use case is presented as Shadow AI
  // and opens the Shadow AI editor instead of seeding the standard one.
  const allPresets = (data?.presets ?? []).map(presentShadowAIUseCase);
  const shadowAISlot = useShadowAIPolicySlot();
  const presets = allPresets.filter(
    (preset) => promptPoliciesEnabled || preset.policyType !== "prompt_based",
  );

  // A description that names an MCP server starts the scope step limited to
  // it, where the scope step offers that and the project has a server to pick.
  const mcpScopeFlag = useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies);
  const serversQuery = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const mcpServers =
    mcpScopeFlag.status === "enabled"
      ? (serversQuery.data?.mcpServers ?? [])
      : [];

  const openDraft = (draft: PolicyDraft) => {
    void navigate(
      { pathname: location.pathname, search: `?kind=${draftKind(draft)}` },
      { state: { draft } },
    );
  };

  // The Shadow AI editor, or the policy already holding the project's one
  // slot. The use-case row disables itself while the slot is unknown; the
  // draft review's Continue cannot, so it reports instead.
  const openShadowAI = () => {
    if (shadowAISlot.pending || shadowAISlot.failed) {
      toast.error(
        "This project's policies could not be checked yet, so the Shadow AI policy cannot be opened. Try again.",
      );
      return;
    }
    if (shadowAISlot.href) {
      void navigate(shadowAISlot.href);
      return;
    }
    void setKind("shadow-ai");
  };

  const continueFromReview = (state: ReviewState) => {
    const override = state.overridePresetId
      ? allPresets.find((preset) => preset.id === state.overridePresetId)
      : undefined;
    const base = override
      ? draftFromPreset(override)
      : draftFromSuggestion(state.suggestion);
    const preset = allPresets.find((p) => p.id === base.presetId);
    if (preset && isShadowAIUseCase(preset)) {
      openShadowAI();
      return;
    }
    const matched = sessionOnlyReason(base.sources)
      ? []
      : matchMCPServers(description, mcpServers);
    openDraft({
      ...base,
      requiresApprovedEmailDomains:
        preset?.requiresApprovedEmailDomains ?? false,
      ...(matched.length > 0
        ? {
            mcpScope: {
              serverIds: matched.map((server) => server.id),
              matchedNames: matched.map(
                (server) => server.name ?? server.slug ?? server.id,
              ),
            },
          }
        : {}),
      origin: {
        source: "describe",
        description: description.trim(),
        suggestion: state.suggestion,
        overridePresetId: state.overridePresetId,
      },
    });
  };

  const suggest = useRiskSuggestPolicyMutation({
    onSuccess: (suggestion) => {
      setReview({ suggestion, overridePresetId: null });
      toast.success(`Drafted "${suggestion.name}"`, {
        description: suggestion.presetId
          ? "Review it, then continue in the wizard."
          : "No use case fits, so it's a prompt-based guardrail. Review it, then continue in the wizard.",
      });
    },
    onError: () =>
      toast.error(
        "Couldn't draft a policy from that description. Try again, or start from a use case.",
      ),
  });
  const trimmed = description.trim();
  const canDescribe = trimmed.length >= 3 && !suggest.isPending;
  const submitDescription = () => {
    if (!canDescribe) return;
    suggest.mutate({
      request: { suggestRiskPolicyRequestBody: { prompt: trimmed } },
    });
  };

  if (review) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <DraftReview
            description={trimmed}
            review={review}
            presets={allPresets}
            mode={mode}
            promptPoliciesEnabled={promptPoliciesEnabled}
            onOverride={(overridePresetId) =>
              setReview({ ...review, overridePresetId })
            }
            onBack={() => setReview(null)}
            onContinue={() => continueFromReview(review)}
          />
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>
            What do you want to protect against?
          </Page.Section.Title>
          <Page.Section.Description>
            Each use case opens the wizard with detectors, action and severity
            set. Every step stays editable.
          </Page.Section.Description>
          <Page.Section.Body>
            <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,1fr)_300px]">
              <UseCaseTable
                presets={presets}
                isLoading={isLoading}
                isError={isError}
                onRetry={() => void refetch()}
                mode={mode}
                promptPoliciesEnabled={promptPoliciesEnabled}
                shadowAISlot={shadowAISlot}
                onSelect={(preset) => openDraft(draftFromPreset(preset))}
                onShadowAI={openShadowAI}
                onScratch={(kind) => void setKind(kind)}
              />
              <DescribePanel
                value={description}
                onChange={setDescription}
                pending={suggest.isPending}
                canSubmit={canDescribe}
                onSubmit={submitDescription}
              />
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function UseCaseTable({
  presets,
  isLoading,
  isError,
  onRetry,
  mode,
  promptPoliciesEnabled,
  shadowAISlot,
  onSelect,
  onShadowAI,
  onScratch,
}: {
  presets: RiskPreset[];
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  mode: DetectorMode;
  promptPoliciesEnabled: boolean;
  shadowAISlot: ShadowAIPolicySlot;
  onSelect: (preset: RiskPreset) => void;
  onShadowAI: () => void;
  onScratch: (kind: "standard" | "prompt" | "shadow-ai") => void;
}): JSX.Element {
  return (
    <div className="bg-card border">
      <div className="bg-muted/30 hidden grid-cols-[minmax(0,1fr)_80px_128px_16px] gap-4 border-b px-4 py-2.5 sm:grid">
        <Eyebrow>Use case</Eyebrow>
        <Eyebrow>On match</Eyebrow>
        <Eyebrow>Severity</Eyebrow>
        <span />
      </div>
      {isError ? (
        <div className="flex flex-wrap items-center gap-3 border-b p-4">
          <Text small muted>
            The use cases could not be loaded.
          </Text>
          <Button type="button" variant="tertiary" onClick={onRetry}>
            Retry
          </Button>
        </div>
      ) : isLoading ? (
        Array.from({ length: 6 }, (_, index) => (
          <div key={index} className="border-b p-4">
            <div className="bg-muted/40 h-16 animate-pulse" />
          </div>
        ))
      ) : (
        presets.map((preset) => {
          // The Shadow AI use case follows the project's one slot: it opens
          // the existing policy when there is one, and waits while that is
          // unknown, the same as the from-scratch option below.
          const shadowAI = isShadowAIUseCase(preset);
          const slotMessage = shadowAI
            ? shadowAISlotMessage(shadowAISlot)
            : undefined;
          return (
            <button
              key={preset.id}
              type="button"
              disabled={slotMessage !== undefined}
              onClick={() => (shadowAI ? onShadowAI() : onSelect(preset))}
              className="hover:bg-muted/40 group grid w-full grid-cols-[minmax(0,1fr)_16px] items-center gap-4 border-b p-4 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60 sm:grid-cols-[minmax(0,1fr)_80px_128px_16px]"
            >
              <span className="flex min-w-0 gap-3">
                <Icon
                  name={presetIcon(preset.id, preset.policyType)}
                  className="text-muted-foreground mt-0.5 size-5 shrink-0"
                />
                <span className="min-w-0 space-y-1">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium">{preset.label}</span>
                    {preset.requiresApprovedEmailDomains ? (
                      <Badge variant="warning">
                        <Badge.Text>Needs approved domains</Badge.Text>
                      </Badge>
                    ) : null}
                    {shadowAI && shadowAISlot.existing ? (
                      <Badge variant="neutral">
                        <Badge.Text>Already set up</Badge.Text>
                      </Badge>
                    ) : null}
                  </span>
                  <span className="text-muted-foreground block max-w-[560px] text-[13px] leading-[18px]">
                    {preset.description}
                  </span>
                  <span className="text-muted-foreground block font-mono text-[11px] leading-4">
                    {slotMessage ??
                      `Detected by ${detectsSummary(preset, mode)}`}
                  </span>
                  <span className="flex items-center gap-2 pt-1 sm:hidden">
                    <ActionBadge action={preset.action} />
                    <Severity score={preset.score} />
                  </span>
                </span>
              </span>
              <span className="hidden sm:block">
                <ActionBadge action={preset.action} />
              </span>
              <span className="hidden sm:block">
                <Severity score={preset.score} />
              </span>
              <ArrowRight className="text-muted-foreground group-hover:text-foreground size-4 transition-colors" />
            </button>
          );
        })
      )}
      <div className="bg-muted/30 flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
        <Eyebrow>From scratch</Eyebrow>
        <Button
          type="button"
          size="sm"
          variant="tertiary"
          onClick={() => onScratch("standard")}
        >
          Built-in detector
        </Button>
        {promptPoliciesEnabled ? (
          <Button
            type="button"
            size="sm"
            variant="tertiary"
            onClick={() => onScratch("prompt")}
          >
            Prompt-based
          </Button>
        ) : null}
        <ShadowAIScratchOption onPick={() => onScratch("shadow-ai")} />
      </div>
    </div>
  );
}

/** Why a Shadow AI policy cannot be opened right now, or undefined when the
 *  slot is known: a pending or failed read is not proof that it is free. */
function shadowAISlotMessage(slot: ShadowAIPolicySlot): string | undefined {
  if (slot.failed) {
    return "This project's policies could not be loaded, so a Shadow AI policy cannot be created yet.";
  }
  if (slot.pending) return "Checking this project's policies…";
  return undefined;
}

/** Why the Shadow AI from-scratch option cannot be picked right now, or
 *  undefined when it can. The option points at the policy already holding the
 *  slot instead of a doomed create. */
function shadowAIScratchNote(slot: ShadowAIPolicySlot): ReactNode {
  const message = shadowAISlotMessage(slot);
  if (message !== undefined) {
    return (
      <Text small muted>
        {message}
      </Text>
    );
  }
  if (slot.existing && slot.href) {
    return (
      <Text small muted>
        This project already has one.{" "}
        <Link to={slot.href} className="underline underline-offset-2">
          Open {slot.existing.name}
        </Link>
      </Text>
    );
  }
  return undefined;
}

/** The "from scratch" entry for a Shadow AI policy: the blocking Shadow MCP
 *  policy re-presented as the place that decides which AI tools and MCP
 *  servers may connect. Exported for its test. */
export function ShadowAIScratchOption({
  onPick,
}: {
  onPick: () => void;
}): JSX.Element {
  const note = shadowAIScratchNote(useShadowAIPolicySlot());
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <Button
        type="button"
        size="sm"
        variant="tertiary"
        disabled={note !== undefined}
        onClick={onPick}
      >
        Shadow AI
      </Button>
      {note}
    </span>
  );
}

function DescribePanel({
  value,
  onChange,
  pending,
  canSubmit,
  onSubmit,
}: {
  value: string;
  onChange: (value: string) => void;
  pending: boolean;
  canSubmit: boolean;
  onSubmit: () => void;
}): JSX.Element {
  return (
    <Stack gap={4} className="bg-card border p-6">
      <Stack gap={1}>
        <Label htmlFor="policy-describe" className="text-lg font-normal">
          Not listed?
        </Label>
        <Text small muted>
          Describe the risk. It maps to the closest use case, or becomes a
          prompt-based guardrail.
        </Text>
      </Stack>
      <TextArea
        id="policy-describe"
        value={value}
        onChange={onChange}
        rows={4}
        placeholder="What should the policy catch or stop?"
      />
      <Stack gap={2}>
        <Eyebrow>Try</Eyebrow>
        {EXAMPLES.map((example) => (
          <button
            key={example}
            type="button"
            onClick={() => onChange(example)}
            className="text-muted-foreground hover:text-foreground text-left text-[13px] leading-[18px] underline-offset-2 hover:underline"
          >
            {example}
          </button>
        ))}
      </Stack>
      <Button
        type="button"
        onClick={onSubmit}
        disabled={!canSubmit}
        className="self-start"
      >
        {pending ? (
          <Loader2 className="size-4 animate-spin" />
        ) : (
          <Sparkles className="size-4" />
        )}
        Draft policy
      </Button>
    </Stack>
  );
}

// ── Review step ──────────────────────────────────────────────────────────────

function DraftReview({
  description,
  review,
  presets,
  mode,
  promptPoliciesEnabled,
  onOverride,
  onBack,
  onContinue,
}: {
  description: string;
  review: ReviewState;
  presets: RiskPreset[];
  mode: DetectorMode;
  promptPoliciesEnabled: boolean;
  onOverride: (presetId: string | null) => void;
  onBack: () => void;
  onContinue: () => void;
}): JSX.Element {
  const { suggestion, overridePresetId } = review;
  const byId = (id: string) => presets.find((preset) => preset.id === id);
  const override = overridePresetId ? byId(overridePresetId) : undefined;
  const suggested = suggestion.presetId ? byId(suggestion.presetId) : undefined;
  const draft = override
    ? draftFromPreset(override)
    : draftFromSuggestion(suggestion);
  const preset = override ?? suggested;
  const bespoke = !preset;
  const fit = override
    ? suggestion.alternatives.find((alt) => alt.presetId === override.id)
        ?.confidence
    : bespoke
      ? undefined
      : suggestion.confidence;
  const blockedByFlag =
    draft.policyType === "prompt_based" && !promptPoliciesEnabled;

  const enabled = (p: RiskPreset | undefined): p is RiskPreset =>
    !!p && (promptPoliciesEnabled || p.policyType !== "prompt_based");
  // The suggested use case stays on offer after an override, so the swap can
  // be undone from the same list.
  const alsoClose = [
    ...(override && suggested
      ? [{ preset: suggested, confidence: suggestion.confidence }]
      : []),
    ...suggestion.alternatives.map((alt) => ({
      preset: byId(alt.presetId),
      confidence: alt.confidence,
    })),
  ].filter(
    (entry): entry is { preset: RiskPreset; confidence: number } =>
      enabled(entry.preset) && entry.preset.id !== preset?.id,
  );

  const eyebrow = override
    ? "Use case you picked"
    : bespoke
      ? "No use case fits"
      : "Closest use case";
  const rationale = override
    ? override.description
    : bespoke
      ? NO_MATCH_RATIONALE
      : suggestion.rationale;

  return (
    <Stack gap={6} className="mx-auto w-full max-w-[800px]">
      <Stack gap={3}>
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={onBack}
          className="self-start"
        >
          <ArrowLeft className="size-4" />
          Edit description
        </Button>
        <Page.Section.Title>Review the draft</Page.Section.Title>
        <Text small muted>
          Nothing is created until you finish the wizard.
        </Text>
      </Stack>

      <Stack gap={1} className="bg-muted/30 border p-4">
        <Eyebrow>You described</Eyebrow>
        <Text>{description}</Text>
      </Stack>

      <div className="bg-card border">
        <div className="flex items-center gap-4 border-b px-6 py-5">
          <IconTile name={presetIcon(draft.presetId, draft.policyType)} />
          <Stack gap={1} className="min-w-0 flex-1">
            <Eyebrow>{eyebrow}</Eyebrow>
            <span className="text-lg leading-6">
              {preset ? preset.label : draft.name}
            </span>
          </Stack>
          {fit !== undefined ? <FitMeter confidence={fit} /> : null}
        </div>
        <Stack gap={4} className="px-6 py-5">
          <Text small muted>
            {rationale}
          </Text>
          <dl className="grid grid-cols-1 sm:grid-cols-[180px_minmax(0,1fr)]">
            <ReviewRow label="Policy type">
              {draft.policyType === "prompt_based"
                ? "Prompt-based (judged by the policy model)"
                : "Built-in detectors"}
            </ReviewRow>
            {draft.policyType === "prompt_based" ? (
              <ReviewRow label="Instruction">
                <span className="whitespace-pre-wrap">{draft.prompt}</span>
              </ReviewRow>
            ) : (
              <ReviewRow label="Detects with">
                {detectsSummary(draft, mode)}
              </ReviewRow>
            )}
            <ReviewRow label="On match">
              <span className="flex flex-wrap items-center gap-2">
                <ActionBadge action={draft.action} />
                {ACTION_LONG_LABEL[draft.action] ?? draft.action}
              </span>
            </ReviewRow>
            <ReviewRow label="Severity">
              <Severity score={draft.score} />
            </ReviewRow>
            <ReviewRow label="Message to user">
              {draft.userMessage ? (
                draft.userMessage
              ) : (
                <span className="text-muted-foreground">
                  None. The user is not interrupted.
                </span>
              )}
            </ReviewRow>
            {preset?.requiresApprovedEmailDomains ? (
              <ReviewRow label="Needs">
                <span className="text-default-warning">
                  Your approved email domains, set in the next step
                </span>
              </ReviewRow>
            ) : null}
          </dl>
          {blockedByFlag ? (
            <Alert variant="warning" alignTop>
              Prompt-based policies aren't switched on for this project.
              {alsoClose.length > 0
                ? " Pick one of the use cases below instead."
                : " Start from a use case instead."}
            </Alert>
          ) : null}
        </Stack>
      </div>

      {alsoClose.length > 0 ? (
        <Stack gap={2}>
          <Eyebrow>Also close</Eyebrow>
          <div className="bg-card border">
            {alsoClose.map(({ preset: alt, confidence }) => (
              <div
                key={alt.id}
                className="flex items-center gap-3 border-b px-4 py-3 last:border-b-0"
              >
                <Icon
                  name={presetIcon(alt.id, alt.policyType)}
                  className="text-muted-foreground size-4 shrink-0"
                />
                <span className="min-w-0 flex-1 truncate text-sm">
                  {alt.label}
                </span>
                <span className="text-muted-foreground font-mono text-xs">
                  {Math.round(confidence * 100)}% fit
                </span>
                <Button
                  type="button"
                  size="sm"
                  variant="tertiary"
                  onClick={() =>
                    onOverride(alt.id === suggested?.id ? null : alt.id)
                  }
                >
                  Use instead
                </Button>
              </div>
            ))}
          </div>
        </Stack>
      ) : null}

      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button type="button" variant="tertiary" onClick={onBack}>
          Browse use cases
        </Button>
        <Button type="button" onClick={onContinue} disabled={blockedByFlag}>
          Continue in wizard
          <ArrowRight className="size-4" />
        </Button>
      </div>
    </Stack>
  );
}

function FitMeter({ confidence }: { confidence: number }): JSX.Element {
  const pct = Math.round(confidence * 100);
  return (
    <Stack gap={1} className="shrink-0 items-end">
      <span className="font-mono text-xs">{pct}% fit</span>
      <span
        className="bg-muted relative h-1 w-24"
        role="meter"
        aria-label="Fit"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <span
          className="bg-foreground absolute inset-y-0 left-0"
          style={{ width: `${pct}%` }}
        />
      </span>
    </Stack>
  );
}

function ReviewRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <>
      <dt className="text-eyebrow border-t py-3 sm:pr-4">{label}</dt>
      <dd className="border-t py-3 text-sm max-sm:border-t-0 max-sm:pt-0">
        {children}
      </dd>
    </>
  );
}

// ── Wizard provenance ────────────────────────────────────────────────────────

const PROVENANCE_DESCRIPTION_LIMIT = 70;

/** Where the wizard's draft came from, with a way back to that starting
 * point. Children render attached underneath, for requirements the draft
 * carries. */
export function PolicyDraftProvenance({
  draft,
  children,
}: {
  draft: PolicyDraft;
  children?: React.ReactNode;
}): JSX.Element | null {
  const navigate = useNavigate();
  const location = useLocation();
  const mode = useDetectorMode();
  const { data } = useRiskPresets();
  const origin = draft.origin;
  if (!origin) return null;
  const preset = data?.presets.find((p) => p.id === draft.presetId);
  const eyebrow =
    origin.source === "describe"
      ? `Drafted from "${truncate(origin.description, PROVENANCE_DESCRIPTION_LIMIT)}"`
      : "Started from a use case";
  const summary = [
    detectsSummary(draft, mode),
    ACTION_LABEL[draft.action] ?? draft.action,
  ].join(" · ");

  return (
    <div className="bg-card border">
      <div className="bg-muted/30 flex flex-wrap items-center gap-3 px-4 py-3">
        <IconTile
          size="sm"
          name={presetIcon(draft.presetId, draft.policyType)}
        />
        <Stack gap={1} className="min-w-0 flex-1">
          <Eyebrow className="truncate">{eyebrow}</Eyebrow>
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
            <span className="font-medium">{preset?.label ?? draft.name}</span>
            <span className="text-muted-foreground">{summary}</span>
            <Severity score={draft.score} />
          </span>
        </Stack>
        <Button
          type="button"
          size="sm"
          variant="secondary"
          onClick={() =>
            void navigate(
              { pathname: location.pathname, search: "" },
              { state: { restore: restoreFromDraft(draft) } },
            )
          }
        >
          Change starting point
        </Button>
      </div>
      {children}
    </div>
  );
}

function truncate(text: string, limit: number): string {
  const trimmed = text.trim();
  return trimmed.length > limit ? `${trimmed.slice(0, limit)}…` : trimmed;
}

/** The approved email domains a non-corporate accounts draft needs before it
 * can detect anything beyond personal AI accounts. */
export function ApprovedDomainsRequirement({
  value,
  onChange,
  invalid,
}: {
  value: string;
  onChange: (value: string) => void;
  invalid: string[];
}): JSX.Element {
  return (
    <div className="border-warning-default flex gap-3 border-t p-4">
      <Icon
        name="triangle-alert"
        className="text-default-warning mt-0.5 size-4 shrink-0"
      />
      <Stack gap={2} className="min-w-0 flex-1">
        <Label
          htmlFor="policy-approved-domains"
          className="text-default-warning text-sm font-medium"
        >
          Add your approved email domains
        </Label>
        <Text small muted>
          Sessions from AI accounts outside these domains are flagged. Until at
          least one domain is set, only the personal-account rule fires.
        </Text>
        <Input
          id="policy-approved-domains"
          value={value}
          onChange={onChange}
          placeholder="e.g. acme.com, corp.acme.com"
          className="max-w-[520px]"
        />
        {invalid.length > 0 ? (
          <Text small destructive>
            {invalid.length === 1
              ? `${invalid[0]} isn't a domain.`
              : `${invalid.join(", ")} aren't domains.`}
          </Text>
        ) : null}
      </Stack>
    </div>
  );
}
