import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import {
  Assistant,
  AssistantStatus,
} from "@gram/client/models/components/assistant.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { Stack } from "@/components/ui/stack";
import { useQueryClient } from "@tanstack/react-query";
import { MouseEvent } from "react";
import { toast } from "sonner";

function stopPropagation(e: MouseEvent<HTMLDivElement>) {
  e.preventDefault();
  e.stopPropagation();
}

/**
 * Active/Paused switch for an assistant. Shared between the Assistants index
 * cards and the detail panel so the control stays identical in both places.
 * `onUpdated` lets a caller refresh its own view (e.g. the detail panel's
 * draft) on top of the assistants-list invalidation this always performs.
 */
export function AssistantStatusToggle({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");
  const isActive = assistant.status === AssistantStatus.Active;

  const updateAssistant = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
    },
    onError: () => {
      toast.error("Failed to update assistant status");
    },
  });

  const handleToggle = () => {
    updateAssistant.mutate({
      request: {
        updateAssistantForm: {
          id: assistant.id,
          status: isActive ? AssistantStatus.Paused : AssistantStatus.Active,
        },
      },
    });
  };

  return (
    <Stack direction="horizontal" gap={2} align="center">
      <div onClick={stopPropagation}>
        <Switch
          checked={isActive}
          onCheckedChange={handleToggle}
          disabled={!canWrite || updateAssistant.isPending}
          aria-label={`${isActive ? "Pause" : "Activate"} assistant ${assistant.name}`}
        />
      </div>
      <Type small muted>
        {isActive ? "Active" : "Paused"}
      </Type>
    </Stack>
  );
}
