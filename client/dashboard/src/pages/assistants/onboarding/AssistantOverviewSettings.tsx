import { AssistantOwner } from "@/components/assistants/assistant-owner";
import { AssistantStatusToggle } from "@/components/assistants/status-toggle";
import { ModelSelect } from "@/components/model-select";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { AVAILABLE_MODELS } from "@/lib/models";
import { Assistant } from "@gram/client/models/components/assistant.js";
import { UpdateAssistantForm } from "@gram/client/models/components/updateassistantform.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Row, Section } from "./PanelSection";

type OverviewDraft = {
  name: string;
  model: string;
  maxConcurrency: string;
  warmTtlSeconds: string;
};

function draftFromAssistant(assistant: Assistant): OverviewDraft {
  return {
    name: assistant.name,
    model: assistant.model,
    maxConcurrency: String(assistant.maxConcurrency),
    warmTtlSeconds: String(assistant.warmTtlSeconds),
  };
}

function modelLabel(model: string): string {
  return AVAILABLE_MODELS.find((m) => m.value === model)?.label ?? model;
}

/**
 * The Overview section of the assistant detail panel. The pencil button turns
 * the rows into a small form; Save appears once something changed, and the X
 * discards the draft and returns to the view state. Status stays a live
 * toggle in both states.
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

  const [draft, setDraft] = useState<OverviewDraft | null>(null);
  const editing = draft !== null;

  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
    },
    onError: () => {
      toast.error("Failed to update assistant");
    },
  });

  const dirty =
    editing &&
    (draft.name.trim() !== assistant.name ||
      draft.model !== assistant.model ||
      draft.maxConcurrency !== String(assistant.maxConcurrency) ||
      draft.warmTtlSeconds !== String(assistant.warmTtlSeconds));

  const setField = (field: keyof OverviewDraft) => (value: string) => {
    setDraft((prev) => (prev ? { ...prev, [field]: value } : prev));
  };

  const validate = (current: OverviewDraft): string | null => {
    if (current.name.trim() === "") {
      return "Name cannot be empty";
    }
    if (
      current.maxConcurrency.trim() === "" ||
      current.warmTtlSeconds.trim() === ""
    ) {
      return "Concurrency and warm TTL cannot be empty";
    }
    const concurrency = Number(current.maxConcurrency);
    if (
      !Number.isInteger(concurrency) ||
      concurrency < 1 ||
      concurrency > 100
    ) {
      return "Concurrency must be a whole number between 1 and 100";
    }
    const warmTtl = Number(current.warmTtlSeconds);
    if (!Number.isInteger(warmTtl) || warmTtl < 0 || warmTtl > 3600) {
      return "Warm TTL must be a whole number between 0 and 3600";
    }
    return null;
  };

  const save = () => {
    if (!draft) return;
    const problem = validate(draft);
    if (problem) {
      toast.error(problem);
      return;
    }
    const form: Omit<UpdateAssistantForm, "id"> = {
      name: draft.name.trim(),
      model: draft.model,
      maxConcurrency: Number(draft.maxConcurrency),
      warmTtlSeconds: Number(draft.warmTtlSeconds),
    };
    update.mutate(
      { request: { updateAssistantForm: { id: assistant.id, ...form } } },
      {
        onSuccess: () => {
          setDraft(null);
          toast.success("Assistant updated");
        },
      },
    );
  };

  const editAction = (
    <Button
      variant="tertiary"
      size="sm"
      className="h-auto gap-1 px-1.5 py-0.5 text-xs"
      aria-label="Edit overview settings"
      onClick={() => setDraft(draftFromAssistant(assistant))}
    >
      <Icon name="pencil" className="h-3 w-3" />
      Edit
    </Button>
  );

  const editingActions = (
    <div className="flex items-center gap-1">
      {dirty && (
        <Button
          size="sm"
          className="h-auto px-2 py-0.5 text-xs"
          onClick={save}
          disabled={update.isPending}
        >
          {update.isPending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            "Save"
          )}
        </Button>
      )}
      <Button
        variant="tertiary"
        size="sm"
        className="h-auto px-1.5 py-0.5"
        aria-label={dirty ? "Discard changes" : "Stop editing"}
        onClick={() => setDraft(null)}
        disabled={update.isPending}
      >
        <Icon name="x" className="h-3 w-3" />
      </Button>
    </div>
  );

  return (
    <Section
      title="Overview"
      action={canWrite ? (editing ? editingActions : editAction) : undefined}
    >
      <Row label="Status">
        <AssistantStatusToggle assistant={assistant} onUpdated={onUpdated} />
      </Row>
      <Row label="Name">
        {editing ? (
          <Input
            value={draft.name}
            onChange={setField("name")}
            maxLength={120}
            aria-label="Name"
            disabled={update.isPending}
            className="h-7 w-48 text-right text-xs"
          />
        ) : (
          <Text small className="truncate">
            {assistant.name}
          </Text>
        )}
      </Row>
      <Row label="Model">
        {editing ? (
          <ModelSelect
            value={draft.model}
            onValueChange={setField("model")}
            disabled={update.isPending}
            triggerClassName="h-7 max-w-[240px] text-xs"
          />
        ) : (
          <Text small>{modelLabel(assistant.model)}</Text>
        )}
      </Row>
      <Row label="Owner">
        <AssistantOwner
          createdByUserId={assistant.createdByUserId}
          variant="row"
        />
      </Row>
      <Row label="Concurrency">
        {editing ? (
          <Input
            type="number"
            value={draft.maxConcurrency}
            onChange={setField("maxConcurrency")}
            min={1}
            max={100}
            aria-label="Concurrency"
            disabled={update.isPending}
            className="h-7 w-20 text-right text-xs"
          />
        ) : (
          <Text small>{assistant.maxConcurrency}</Text>
        )}
      </Row>
      <Row label="Warm TTL">
        {editing ? (
          <span className="flex items-center gap-1">
            <Input
              type="number"
              value={draft.warmTtlSeconds}
              onChange={setField("warmTtlSeconds")}
              min={0}
              max={3600}
              aria-label="Warm TTL in seconds"
              disabled={update.isPending}
              className="h-7 w-20 text-right text-xs"
            />
            <Text small muted>
              s
            </Text>
          </span>
        ) : (
          <Text small>{assistant.warmTtlSeconds}s</Text>
        )}
      </Row>
    </Section>
  );
}
