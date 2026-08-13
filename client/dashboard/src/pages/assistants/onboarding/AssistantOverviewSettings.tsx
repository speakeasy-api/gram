import { AssistantOwner } from "@/components/assistants/assistant-owner";
import { AssistantStatusToggle } from "@/components/assistants/status-toggle";
import { InlineEditableText } from "@/components/inline-editable-text";
import { ModelSelect } from "@/components/model-select";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { Assistant } from "@gram/client/models/components/assistant.js";
import { UpdateAssistantForm } from "@gram/client/models/components/updateassistantform.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Row, Section } from "./PanelSection";

/**
 * The editable Overview section of the assistant detail panel. Every setting
 * the update endpoint accepts is editable in place: name, concurrency, and
 * warm TTL commit on blur/Enter; status and model apply immediately.
 */
export function AssistantOverviewSettings({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");

  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
    },
    onError: () => {
      toast.error("Failed to update assistant");
    },
  });

  const save = async (form: Omit<UpdateAssistantForm, "id">) => {
    await update.mutateAsync({
      request: { updateAssistantForm: { id: assistant.id, ...form } },
    });
  };

  const disabled = !canWrite || update.isPending;

  return (
    <Section title="Overview">
      <Row label="Status">
        <AssistantStatusToggle assistant={assistant} onUpdated={onUpdated} />
      </Row>
      <Row label="Name">
        <InlineEditableText
          value={assistant.name}
          onSubmit={async (name) => {
            try {
              await save({ name });
              return true;
            } catch {
              return false;
            }
          }}
          inputLabel="Assistant name"
          editTitle="Edit assistant name"
          maxLength={120}
          disabled={disabled}
          editorClassName="h-7 w-48"
          inputClassName="text-right text-xs"
        >
          <Text small className="truncate">
            {assistant.name}
          </Text>
        </InlineEditableText>
      </Row>
      <Row label="Model">
        <ModelSelect
          value={assistant.model}
          onValueChange={(model) => void save({ model }).catch(() => {})}
          disabled={disabled}
          ariaLabel="Model"
          triggerClassName="h-7 max-w-[240px] text-xs"
        />
      </Row>
      <Row label="Owner">
        <AssistantOwner
          createdByUserId={assistant.createdByUserId}
          variant="row"
        />
      </Row>
      <Row label="Concurrency">
        <EditableNumberValue
          value={assistant.maxConcurrency}
          min={1}
          max={100}
          ariaLabel="Concurrency"
          disabled={disabled}
          onCommit={(maxConcurrency) => save({ maxConcurrency })}
        />
      </Row>
      <Row label="Warm TTL">
        <EditableNumberValue
          value={assistant.warmTtlSeconds}
          min={0}
          max={3600}
          suffix="s"
          ariaLabel="Warm TTL in seconds"
          disabled={disabled}
          onCommit={(warmTtlSeconds) => save({ warmTtlSeconds })}
        />
      </Row>
    </Section>
  );
}

/**
 * An inline integer input that commits on blur or Enter and reverts on
 * Escape, clamped to [min, max] with a toast on invalid input. A failed
 * commit keeps the draft so the edit isn't lost.
 */
function EditableNumberValue({
  value,
  min,
  max,
  suffix,
  ariaLabel,
  disabled,
  onCommit,
}: {
  value: number;
  min: number;
  max: number;
  suffix?: string;
  ariaLabel: string;
  disabled: boolean;
  onCommit: (value: number) => Promise<void>;
}): JSX.Element {
  const [draft, setDraft] = useState<string | null>(null);

  const commit = async () => {
    if (draft === null) return;
    if (draft.trim() === "") {
      setDraft(null);
      return;
    }
    const parsed = Number(draft);
    if (parsed === value) {
      setDraft(null);
      return;
    }
    if (!Number.isInteger(parsed) || parsed < min || parsed > max) {
      setDraft(null);
      toast.error(`Value must be a whole number between ${min} and ${max}`);
      return;
    }
    try {
      await onCommit(parsed);
      setDraft(null);
    } catch {
      // The mutation's onError toast already fired; keep the draft so the
      // user's edit isn't lost.
    }
  };

  return (
    <span className="flex items-center gap-1">
      <Input
        type="number"
        aria-label={ariaLabel}
        value={draft ?? String(value)}
        onChange={setDraft}
        onBlur={() => void commit()}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
          if (e.key === "Escape") setDraft(null);
        }}
        min={min}
        max={max}
        disabled={disabled}
        className="h-7 w-20 text-right text-xs"
      />
      {suffix && (
        <Text small muted>
          {suffix}
        </Text>
      )}
    </span>
  );
}
