// Prompt-based policy wizard body + judge config (AGE-2704).
// Moved verbatim from PolicyCenter.tsx.

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Badge } from "@speakeasy-api/moonshine";
import { ChevronRight, Info, SlidersHorizontal } from "lucide-react";
import { useState } from "react";
import { type PolicyAction, type PolicyMessageType } from "../policy-data";
import { ALL_POLICY_MESSAGE_TYPES } from "../policy-display";
import { PROMPT_POLICY_TEMPLATES } from "../prompt-policy-templates";
import { ActionPicker, ScopeCard } from "./risk-policy-body";
import { FormLayout, FormSection } from "./wizard-chrome";
import { PROMPT_FORM_SECTIONS } from "./wizard-steps";
import { DEFAULT_JUDGE_TEMPERATURE } from "./payload";

const TEMPERATURE_MIN = 0;
const TEMPERATURE_MAX = 1;
const TEMPERATURE_STEP = 0.1;
// Discrete stops rendered on the slider track: 0.0, 0.1, … 1.0.
const TEMPERATURE_TICKS = Array.from(
  { length: 11 },
  (_, i) => Math.round(i * TEMPERATURE_STEP * 10) / 10,
);

// JUDGE_MODEL_OPTIONS lists the models a prompt policy may run its LLM judge on.
// The recommended option uses the empty value, which follows the server's
// default judge model. Its label names that model and must stay in sync with
// riskjudge.defaultJudgeModel. See server/cmd/riskjudgebench for the benchmark
// behind these picks.
const JUDGE_MODEL_OPTIONS: {
  value: string;
  label: string;
  description: string;
  recommended?: boolean;
}[] = [
  {
    value: "",
    label: "Gemini 3.1 Flash Lite",
    description:
      "Fast, low-cost, high-recall classifier. Best fit for most policies.",
    recommended: true,
  },
  {
    value: "google/gemini-2.5-flash",
    label: "Gemini 2.5 Flash",
    description: "Lowest latency. Good for very high-volume policies.",
  },
  {
    value: "anthropic/claude-sonnet-4.6",
    label: "Claude Sonnet 4.6",
    description:
      "Strong quality and the highest ceiling, at higher latency and cost.",
  },
  {
    value: "anthropic/claude-haiku-4.5",
    label: "Claude Haiku 4.5",
    description: "Balanced Anthropic option.",
  },
];

function promptTemplateNameForInstruction(prompt: string): string | undefined {
  return PROMPT_POLICY_TEMPLATES.find((template) => template.prompt === prompt)
    ?.name;
}

export function PromptPolicySheetBody({
  isEditing,
  formName,
  setFormName,
  formPromptInstruction,
  setFormPromptInstruction,
  formAction,
  setFormAction,
  formAutoName,
  setFormAutoName,
  formEnabled,
  setFormEnabled,
  formModel,
  setFormModel,
  formTemperature,
  setFormTemperature,
  formFailOpen,
  setFormFailOpen,
  selectedMessageTypes,
  setSelectedMessageTypes,
}: {
  isEditing: boolean;
  formName: string;
  setFormName: (v: string) => void;
  formPromptInstruction: string;
  setFormPromptInstruction: (v: string) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  formModel: string;
  setFormModel: (v: string) => void;
  formTemperature: number;
  setFormTemperature: (v: number) => void;
  formFailOpen: boolean;
  setFormFailOpen: (v: boolean) => void;
  selectedMessageTypes: Set<PolicyMessageType>;
  setSelectedMessageTypes: (v: Set<PolicyMessageType>) => void;
}): JSX.Element {
  const [selectedExampleName, setSelectedExampleName] = useState(
    () => promptTemplateNameForInstruction(formPromptInstruction) ?? "",
  );
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const handlePromptChange = (value: string) => {
    setSelectedExampleName("");
    setFormPromptInstruction(value);
  };

  // One-line view of the judge config shown on the collapsed Advanced card, so
  // authors can see the (sensible) defaults at a glance without expanding it.
  const judgeModelLabel =
    JUDGE_MODEL_OPTIONS.find((o) => o.value === formModel)?.label ??
    (formModel || JUDGE_MODEL_OPTIONS[0]?.label) ??
    "Default model";
  const judgeSummary = `${judgeModelLabel} · temp ${formTemperature.toFixed(1)} · ${formFailOpen ? "fail-open" : "fail-closed"}`;

  return (
    <FormLayout sections={PROMPT_FORM_SECTIONS}>
      <FormSection
        id="guardrail"
        title="What should this policy catch?"
        description="Describe the behavior to detect in plain language; the LLM judge evaluates each in-scope message against it."
      >
        <div className="space-y-6">
          <PromptPolicyHowItWorks isEditing={isEditing} />
          <div className="space-y-2">
            <Label className="text-sm font-medium">Policy Prompt</Label>
            <TextArea
              value={formPromptInstruction}
              onChange={handlePromptChange}
              placeholder="Describe the tool-call behavior this policy should match..."
              rows={5}
            />
            {!isEditing && (
              <PromptExampleChips
                selectedExampleName={selectedExampleName}
                onSelect={(template) => {
                  setSelectedExampleName(template.name);
                  setFormPromptInstruction(template.prompt);
                  if (!formName.trim()) {
                    setFormName(template.name);
                  }
                }}
              />
            )}
          </div>
          <Collapsible
            open={advancedOpen}
            onOpenChange={setAdvancedOpen}
            className="border-border rounded-lg border"
          >
            <CollapsibleTrigger className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors">
              <SlidersHorizontal className="text-muted-foreground h-4 w-4 shrink-0" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">
                    Advanced judge settings
                  </span>
                  <Badge variant="neutral" className="text-[10px]">
                    <Badge.Text>Optional</Badge.Text>
                  </Badge>
                </div>
                <div className="text-muted-foreground truncate text-xs">
                  {advancedOpen
                    ? "Judge model, temperature, and failure behavior"
                    : judgeSummary}
                </div>
              </div>
              <ChevronRight
                className={cn(
                  "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
                  advancedOpen && "rotate-90",
                )}
              />
            </CollapsibleTrigger>
            <CollapsibleContent className="border-border border-t px-4 py-4">
              <JudgeConfigSection
                formModel={formModel}
                setFormModel={setFormModel}
                formTemperature={formTemperature}
                setFormTemperature={setFormTemperature}
                formFailOpen={formFailOpen}
                setFormFailOpen={setFormFailOpen}
              />
            </CollapsibleContent>
          </Collapsible>
        </div>
      </FormSection>

      <FormSection
        id="scope"
        title="Where should it evaluate?"
        description="Narrow the scope to control cost — a prompt policy runs the LLM judge on each in-scope message."
      >
        <div className="space-y-6">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {ALL_POLICY_MESSAGE_TYPES.map((type) => (
              <ScopeCard
                key={type}
                type={type as PolicyMessageType}
                checked={selectedMessageTypes.has(type as PolicyMessageType)}
                onToggle={(checked) => {
                  const updated = new Set(selectedMessageTypes);
                  if (checked) {
                    updated.add(type as PolicyMessageType);
                  } else {
                    updated.delete(type as PolicyMessageType);
                  }
                  setSelectedMessageTypes(updated);
                }}
              />
            ))}
          </div>
          {selectedMessageTypes.size === 0 && (
            <p className="text-destructive text-xs">
              Select at least one session part.
            </p>
          )}
        </div>
      </FormSection>

      <FormSection
        id="action"
        title="What happens on a match?"
        description="Choose how the policy responds when the judge flags a message."
      >
        <ActionPicker formAction={formAction} setFormAction={setFormAction} />
      </FormSection>

      <FormSection
        id="details"
        title="Name & enable"
        description="Name the policy and choose whether it enforces the prompt."
      >
        <div className="space-y-6">
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">Policy Name</Label>
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground text-xs">Auto</span>
                <Switch
                  checked={formAutoName}
                  onCheckedChange={setFormAutoName}
                />
              </div>
            </div>
            {formAutoName ? (
              <p className="text-muted-foreground text-xs">
                Name will be generated from the policy prompt.
              </p>
            ) : (
              <>
                <Input
                  value={formName}
                  onChange={setFormName}
                  placeholder="e.g. No Production Deletes"
                />
                {!formName.trim() && (
                  <p className="text-destructive text-xs">Name is required.</p>
                )}
              </>
            )}
          </div>
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-sm font-medium">Enabled</Label>
              <p className="text-muted-foreground text-xs">
                Enable this policy to enforce the prompt.
              </p>
            </div>
            <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
          </div>
        </div>
      </FormSection>
    </FormLayout>
  );
}

function JudgeConfigSection({
  formModel,
  setFormModel,
  formTemperature,
  setFormTemperature,
  formFailOpen,
  setFormFailOpen,
}: {
  formModel: string;
  setFormModel: (v: string) => void;
  formTemperature: number;
  setFormTemperature: (v: number) => void;
  formFailOpen: boolean;
  setFormFailOpen: (v: boolean) => void;
}) {
  return (
    <div className="space-y-4">
      <JudgeModelPicker formModel={formModel} setFormModel={setFormModel} />

      <div className="border-border space-y-4 rounded-lg border p-4">
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Label className="text-sm font-medium">Temperature</Label>
            <span className="text-muted-foreground text-xs tabular-nums">
              {formTemperature.toFixed(1)}
              {formTemperature === DEFAULT_JUDGE_TEMPERATURE
                ? " · default"
                : ""}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-foreground text-xs tabular-nums">0</span>
            <div className="flex-1">
              <Slider
                value={formTemperature}
                onChange={(v) => setFormTemperature(Math.round(v * 10) / 10)}
                min={TEMPERATURE_MIN}
                max={TEMPERATURE_MAX}
                step={TEMPERATURE_STEP}
                ticks={TEMPERATURE_TICKS}
              />
            </div>
            <span className="text-foreground text-xs tabular-nums">1</span>
          </div>
          <p className="text-muted-foreground text-xs">
            Lower is more consistent and repeatable (0 recommended). Higher adds
            variation, which can surface borderline or unusual violations a
            rigid read might miss.
          </p>
        </div>

        <div className="border-border flex items-start justify-between gap-4 border-t pt-4">
          <div className="space-y-2 pr-2">
            <Label className="text-sm font-medium">Fail open</Label>
            <p className="text-muted-foreground text-xs">
              When the judge errors or times out, allow the message instead of
              blocking it.
            </p>
          </div>
          <Switch checked={formFailOpen} onCheckedChange={setFormFailOpen} />
        </div>
      </div>
    </div>
  );
}

function JudgeModelPicker({
  formModel,
  setFormModel,
}: {
  formModel: string;
  setFormModel: (v: string) => void;
}) {
  // A model persisted via the API but not in the curated list still needs to
  // round-trip, so surface it as a selectable option.
  const knownValues = JUDGE_MODEL_OPTIONS.map((o) => o.value);
  const options =
    formModel && !knownValues.includes(formModel)
      ? [
          ...JUDGE_MODEL_OPTIONS,
          {
            value: formModel,
            label: formModel,
            description: "Custom model configured via the API.",
          },
        ]
      : JUDGE_MODEL_OPTIONS;

  return (
    <div className="space-y-2">
      <Label className="text-sm font-medium">Model</Label>
      <RadioGroup value={formModel} onValueChange={setFormModel}>
        <div className="border-border divide-border divide-y rounded-lg border">
          {options.map((opt) => (
            <label
              key={opt.value || "recommended"}
              htmlFor={`judge-model-${opt.value || "recommended"}`}
              className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 p-3"
            >
              <RadioGroupItem
                value={opt.value}
                id={`judge-model-${opt.value || "recommended"}`}
                className="mt-0.5"
              />
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{opt.label}</span>
                  {opt.recommended && (
                    <Badge variant="neutral" className="text-[10px]">
                      <Badge.Text>Recommended</Badge.Text>
                    </Badge>
                  )}
                </div>
                <div className="text-muted-foreground mt-1 text-xs">
                  {opt.description}
                </div>
              </div>
            </label>
          ))}
        </div>
      </RadioGroup>
    </div>
  );
}

function PromptExampleChips({
  selectedExampleName,
  onSelect,
}: {
  selectedExampleName: string;
  onSelect: (template: (typeof PROMPT_POLICY_TEMPLATES)[number]) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-muted-foreground text-xs">Try:</span>
      {PROMPT_POLICY_TEMPLATES.map((template) => {
        const active = selectedExampleName === template.name;
        return (
          <SimpleTooltip key={template.name} tooltip={template.prompt}>
            <button
              type="button"
              aria-pressed={active}
              onClick={() => onSelect(template)}
              className={cn(
                "rounded-full border px-2.5 py-1 text-xs transition-colors",
                active
                  ? "border-foreground/30 bg-muted text-foreground"
                  : "border-border text-muted-foreground hover:bg-muted/50 hover:text-foreground",
              )}
            >
              {template.name}
            </button>
          </SimpleTooltip>
        );
      })}
    </div>
  );
}

function PromptPolicyHowItWorks({ isEditing }: { isEditing: boolean }) {
  const [open, setOpen] = useState(!isEditing);

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className="border-border rounded-lg border"
    >
      <CollapsibleTrigger className="hover:bg-muted/40 flex w-full items-start gap-3 px-4 py-3 text-left transition-colors">
        <Info className="text-muted-foreground mt-0.5 h-4 w-4 shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">How this works</div>
          <div className="text-muted-foreground text-xs">
            An LLM judge reads each in-scope message and flags it when it
            matches your prompt.
          </div>
        </div>
        <ChevronRight
          className={cn(
            "text-muted-foreground mt-0.5 h-4 w-4 shrink-0 transition-transform",
            open && "rotate-90",
          )}
        />
      </CollapsibleTrigger>
      <CollapsibleContent className="border-border border-t px-4 py-3">
        <p className="text-muted-foreground text-sm">
          Prompt-based policies call an LLM judge, so every in-scope message
          adds latency and cost versus the deterministic detection rules. The
          judge sees one message at a time — a tool call and its inputs, or
          message content — never the whole conversation. Narrow <b>Scope</b> to
          control which messages it runs on, and add <b>Exemptions</b> to skip
          trusted ones and keep cost down.
        </p>
      </CollapsibleContent>
    </Collapsible>
  );
}
