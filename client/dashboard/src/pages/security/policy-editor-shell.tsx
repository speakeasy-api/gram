import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Heading } from "@/components/ui/Heading";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Slider } from "@/components/ui/Slider";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { Check, Pencil } from "lucide-react";
import { Fragment, type ReactNode, useState } from "react";
import { ActionPicker, PolicyAudiencePicker } from "./PolicyCenter";
import type { PolicyAction } from "./policy-data";
import {
  type PolicyEditorKind,
  POLICY_EDITOR_KIND_META,
} from "./policy-editor-kind";
import { policyStatusLabel } from "./policy-enabled";
import { SeverityBadge } from "./risk-ui";
import type { PolicyStep } from "./use-step-param";
import {
  togglePolicyEnabledVariables,
  useTogglePolicyEnabled,
} from "./use-toggle-policy-enabled";

// The pieces every policy editor is built from: the stepper shell and its
// Back/Continue footer, the pinned header, and the Action step. They live
// apart from the standard and prompt editors so a third editor can use them
// without importing the 4,000-line file those two share.

const POLICY_ACTION_MESSAGE_FIELD_ID = "policy-action-message";

// An approval policy shows the agent how to request approval, not a message
// of its own.
const POLICY_ACTION_MESSAGE: Record<
  Exclude<PolicyAction, "flag" | "require_approval">,
  { label: string; description: string; placeholder: string }
> = {
  warn: {
    label: "Warning message",
    description:
      "Shown to the user when this policy warns on a tool call or prompt. Supports %{match}, %{entity}, %{policy}, and %{rule} placeholders, substituted at warn time. Leave blank to use the default message.",
    placeholder: "e.g. %{match} looks sensitive. Acknowledge to proceed.",
  },
  block: {
    label: "Block message",
    description:
      "Shown to the user when this policy blocks a tool call or prompt. Leave blank to use the default message.",
    placeholder:
      "e.g. This action was blocked by your organization's security policy. Contact your admin for help.",
  },
  quarantine: {
    label: "Quarantine message",
    description:
      "Shown to the user when this policy quarantines their session. Leave blank to use the default message.",
    placeholder:
      "e.g. This session was quarantined by your organization's security policy. Contact your admin for help.",
  },
};

// ── Stepper shell (pinned header + left rail + spacious content column) ───────

// Horizontal stepper across the top: a left rail competes with the app's own
// sidebar, so the step nav runs horizontally and the step content gets the full
// width below it. Free navigation — any step is clickable.
function HorizontalStepper({
  steps,
  current,
  onStep,
  prefilledStepIds,
}: {
  steps: PolicyStep[];
  current: number;
  onStep: (index: number) => void;
  prefilledStepIds?: ReadonlySet<string>;
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
              aria-current={isCurrent ? "step" : undefined}
              onClick={() => onStep(index)}
              className="group flex shrink-0 items-center gap-2 py-1 pr-1 text-left"
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
                  "sr-only text-sm font-medium sm:not-sr-only sm:inline",
                  isCurrent ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {step.title}
              </span>
              {prefilledStepIds?.has(step.id) ? (
                <span className="border-border bg-card text-muted-foreground hidden border px-1 py-0.5 font-mono text-[10px] leading-none tracking-[0.1em] uppercase sm:inline">
                  Prefilled
                </span>
              ) : null}
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

export function StepperShell({
  header,
  provenance,
  steps,
  current,
  onStep,
  prefilledStepIds,
  children,
}: {
  header: React.ReactNode;
  /** Where a prefilled draft came from, shown between header and stepper. */
  provenance?: React.ReactNode;
  steps: PolicyStep[];
  current: number;
  onStep: (index: number) => void;
  prefilledStepIds?: ReadonlySet<string>;
  children: React.ReactNode;
}): JSX.Element {
  return (
    // Full width — Page.Body already centers content at max-w-7xl (the app's
    // standard page width).
    <Stack gap={6} className="w-full">
      {header}
      {provenance}
      <div className="bg-muted/20 border px-4 py-3">
        <HorizontalStepper
          steps={steps}
          current={current}
          onStep={onStep}
          prefilledStepIds={prefilledStepIds}
        />
      </div>
      <Stack gap={6}>{children}</Stack>
    </Stack>
  );
}

// Back / Continue footer for stepping through sections; the left rail also
// allows free navigation.
export function StepNav({
  step,
  count,
  onStep,
  continueDisabled = false,
}: {
  step: number;
  count: number;
  onStep: (index: number) => void;
  continueDisabled?: boolean;
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
        <Button
          variant="secondary"
          disabled={continueDisabled}
          onClick={() => onStep(step + 1)}
        >
          <Button.Text>Continue</Button.Text>
        </Button>
      ) : (
        <span />
      )}
    </Stack>
  );
}

// ── Shared header (pinned, both kinds) ───────────────────────────────────────

export function PolicyHeader({
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
  kind: PolicyEditorKind;
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
  const {
    icon: KindIcon,
    label: kindLabel,
    placeholder,
    createHint,
  } = POLICY_EDITOR_KIND_META[kind];
  const isCreate = policy === null;
  const routes = useRoutes();
  const [editingName, setEditingName] = useState(false);
  const toggleEnabledMutation = useTogglePolicyEnabled();

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
          {policy ? (
            <>
              <StatusBadge enabled={policy.enabled} />
              <Switch
                checked={policy.enabled}
                disabled={saving || toggleEnabledMutation.isPending}
                onCheckedChange={(checked) =>
                  toggleEnabledMutation.mutate(
                    togglePolicyEnabledVariables(
                      policy.id,
                      policy.name,
                      checked,
                    ),
                  )
                }
                aria-label={policy.enabled ? "Disable policy" : "Enable policy"}
              />
            </>
          ) : null}
        </Stack>
        {policy ? (
          <Text small muted>
            Version {policy.version} · {kindLabel}
            {policy.enabled
              ? null
              : " · Inactive — new messages are not scanned"}
          </Text>
        ) : (
          <Text small muted>
            {createHint}
          </Text>
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

function StatusBadge({ enabled }: { enabled: boolean }): JSX.Element {
  return (
    <Badge variant={enabled ? "success" : "neutral"}>
      {policyStatusLabel(enabled)}
    </Badge>
  );
}

// Vertical section header — title stacked over subtext with breathing room.
// (Card.Header lays title + description side-by-side, which reads cramped.)
// Title is optional: the step name already shows in the stepper, so primary
// step sections pass description only (avoids repeating "Guardrail"/"Scope"/…
// in both the progress rail and the card). Sub-sections (e.g. Judge) keep a
// title since they aren't named by the stepper.
export function SectionHeader({
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
        <Text small muted>
          {description}
        </Text>
      ) : null}
    </div>
  );
}

// ── Action section (flag vs block) ───────────────────────────────────────────

// Shared Action step — identical for prompt and standard: flag/block picker,
// audience, and the block-time custom message.
// Severity assigns a CVSS-style score (0.1–10, default 5) to the policy.
// Findings inherit it at read time to render a severity badge. Pure metadata —
// changing it never re-scans or regenerates findings.
const SEVERITY_MIN = 0.1;
const SEVERITY_MAX = 10;

export function SeveritySection({
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
          <Text small mono>
            {score.toFixed(1)}
          </Text>
          <SeverityBadge score={score} />
        </Stack>
      </div>
      <Text small muted>
        Rate how severe this policy's findings are, from {SEVERITY_MIN} to{" "}
        {SEVERITY_MAX}. Findings surface this as a severity badge; it does not
        change what the policy detects.
      </Text>
      <div className="pt-3">
        <Slider
          aria-label="Severity"
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

export function ActionStep({
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
  mcpScoped = false,
  pinnedAction,
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
  mcpScoped?: boolean;
  pinnedAction?: { value: PolicyAction; reason: string };
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
            mcpScoped={mcpScoped}
            pinnedAction={pinnedAction}
          />
          {shadowMCPAllowedServers}
          <PolicyAudiencePicker
            formAudienceType={audienceType}
            setFormAudienceType={setAudienceType}
            selectedAudiencePrincipalUrns={audiencePrincipalUrns}
            setSelectedAudiencePrincipalUrns={setAudiencePrincipalUrns}
          />
          {action !== "flag" && action !== "require_approval" && (
            <ActionMessageField
              action={action}
              userMessage={userMessage}
              setUserMessage={setUserMessage}
            />
          )}
        </Stack>
      </Card>
    </div>
  );
}

/** The message a user sees when an interrupting action fires. The label and
 *  hint follow the action unless an editor has something more exact to say. */
export function ActionMessageField({
  action,
  userMessage,
  setUserMessage,
  label = POLICY_ACTION_MESSAGE[action].label,
  description = POLICY_ACTION_MESSAGE[action].description,
}: {
  action: Exclude<PolicyAction, "flag" | "require_approval">;
  userMessage: string;
  setUserMessage: React.Dispatch<React.SetStateAction<string>>;
  label?: string;
  description?: string;
}): JSX.Element {
  return (
    <div className="space-y-2">
      <Label
        className="text-sm font-medium"
        htmlFor={POLICY_ACTION_MESSAGE_FIELD_ID}
      >
        {label}
      </Label>
      <p className="text-muted-foreground text-xs">{description}</p>
      <TextArea
        id={POLICY_ACTION_MESSAGE_FIELD_ID}
        value={userMessage}
        onChange={setUserMessage}
        placeholder={POLICY_ACTION_MESSAGE[action].placeholder}
        rows={3}
      />
    </div>
  );
}

/** One label/value line of a Review step. */
export function SummaryRow({
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
      <Text small muted className="shrink-0">
        {label}
      </Text>
      <div className="min-w-0">{children}</div>
    </Stack>
  );
}
