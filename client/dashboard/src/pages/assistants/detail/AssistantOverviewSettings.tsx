import { ModelSelect } from "@/components/model-select";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { AssistantRuntimeSettings } from "./AssistantRuntimeSettings";
import { Section } from "./PanelSection";
import {
  KIT_REASONING_EFFORT_HELP,
  REASONING_EFFORT_HELP,
  type ReasoningEffort,
  reasoningEffortOrDefault,
} from "./reasoning-effort";
import { ReasoningEffortSelect } from "./ReasoningEffortSelect";
import { ASSISTANT_GENERAL_SECTION_ID } from "./sectionIds";

type GeneralDraft = {
  name: string;
  model: string;
  reasoningEffort: ReasoningEffort;
  maxConcurrency: string;
  warmTtlSeconds: string;
};

function draftFromAssistant(assistant: Assistant): GeneralDraft {
  return {
    name: assistant.name,
    model: assistant.model,
    reasoningEffort: reasoningEffortOrDefault(assistant.reasoningEffort),
    maxConcurrency: String(assistant.maxConcurrency),
    warmTtlSeconds: String(assistant.warmTtlSeconds),
  };
}

function problemWith(draft: GeneralDraft, isKit: boolean): string | null {
  if (isKit && draft.reasoningEffort === "none") {
    return KIT_REASONING_EFFORT_HELP;
  }
  if (draft.name.trim() === "") return "Name can't be empty.";
  const concurrency = Number(draft.maxConcurrency);
  if (
    draft.maxConcurrency.trim() === "" ||
    !Number.isInteger(concurrency) ||
    concurrency < 1 ||
    concurrency > 100
  ) {
    return "Concurrency must be a whole number from 1 to 100.";
  }
  const warmTtl = Number(draft.warmTtlSeconds);
  if (
    draft.warmTtlSeconds.trim() === "" ||
    !Number.isInteger(warmTtl) ||
    warmTtl < 0 ||
    warmTtl > 3600
  ) {
    return "Warm TTL must be a whole number from 0 to 3600.";
  }
  return null;
}

/**
 * Name, model and runtime limits as one form. Everything waits for Save;
 * Discard puts the saved values back.
 */
export function AssistantOverviewSettings({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("assistant:write", assistant.id, project.id);
  const isKit = assistant.runtimeVersion === 3;

  const saved = draftFromAssistant(assistant);
  const [draft, setDraft] = useState<GeneralDraft>(saved);
  // A save from elsewhere resets the form to what is now stored.
  const savedKey = JSON.stringify(saved);
  const [savedAt, setSavedAt] = useState(savedKey);
  if (savedAt !== savedKey) {
    setSavedAt(savedKey);
    setDraft(saved);
  }

  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
      toast.success("Settings saved");
    },
    onError: () => toast.error("Couldn't save the settings. Try again."),
  });

  const dirty = (Object.keys(saved) as (keyof GeneralDraft)[]).some(
    (key) => draft[key] !== saved[key],
  );
  const problem = dirty ? problemWith(draft, isKit) : null;
  const disabled = !canWrite || update.isPending;
  const set = (key: keyof GeneralDraft) => (value: string) =>
    setDraft((prev) => ({ ...prev, [key]: value }));

  const save = () => {
    if (!dirty || problem || disabled) return;
    update.mutate({
      request: {
        updateAssistantForm: {
          id: assistant.id,
          name: draft.name.trim(),
          model: draft.model,
          reasoningEffort: draft.reasoningEffort,
          maxConcurrency: Number(draft.maxConcurrency),
          warmTtlSeconds: Number(draft.warmTtlSeconds),
        },
      },
    });
  };

  return (
    <Section
      id={ASSISTANT_GENERAL_SECTION_ID}
      title="General"
      footer={
        canWrite ? (
          <>
            <Text small className="text-destructive">
              {problem}
            </Text>
            <div className="ml-auto flex items-center gap-2">
              {dirty ? (
                <Button
                  variant="tertiary"
                  onClick={() => setDraft(saved)}
                  disabled={update.isPending}
                >
                  Discard
                </Button>
              ) : null}
              <Button
                onClick={save}
                disabled={!dirty || !!problem || update.isPending}
              >
                {update.isPending ? "Saving…" : "Save"}
              </Button>
            </div>
          </>
        ) : (
          <Text small muted>
            Read-only.
          </Text>
        )
      }
    >
      <div className="grid max-w-3xl gap-x-6 gap-y-5 sm:grid-cols-2">
        <Field id="assistant-name" label="Name">
          <Input
            id="assistant-name"
            aria-label="Name"
            value={draft.name}
            onChange={set("name")}
            maxLength={120}
            disabled={disabled}
          />
        </Field>
        <Field id="assistant-model" label="Model">
          <ModelSelect
            id="assistant-model"
            value={draft.model}
            onValueChange={set("model")}
            disabled={disabled}
            triggerClassName="w-full"
          />
        </Field>
        <Field
          id="assistant-reasoning"
          label="Reasoning effort"
          hint={isKit ? KIT_REASONING_EFFORT_HELP : REASONING_EFFORT_HELP}
        >
          <ReasoningEffortSelect
            id="assistant-reasoning"
            disabledOff={isKit}
            value={draft.reasoningEffort}
            onValueChange={set("reasoningEffort")}
            disabled={disabled}
          />
        </Field>
        <div className="grid grid-cols-2 gap-x-6">
          <Field id="assistant-concurrency" label="Concurrency">
            <Input
              id="assistant-concurrency"
              aria-label="Concurrency"
              type="number"
              min={1}
              max={100}
              value={draft.maxConcurrency}
              onChange={set("maxConcurrency")}
              disabled={disabled}
            />
          </Field>
          <Field id="assistant-warm-ttl" label="Warm TTL (seconds)">
            <Input
              id="assistant-warm-ttl"
              aria-label="Warm TTL in seconds"
              type="number"
              min={0}
              max={3600}
              value={draft.warmTtlSeconds}
              onChange={set("warmTtlSeconds")}
              disabled={disabled}
            />
          </Field>
        </div>
      </div>
      <div className="border-border max-w-3xl border-t pt-4">
        <AssistantRuntimeSettings
          assistant={assistant}
          canWrite={canWrite}
          onUpdated={onUpdated}
        />
      </div>
    </Section>
  );
}

function Field({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint ? (
        <Text small muted>
          {hint}
        </Text>
      ) : null}
    </div>
  );
}
