import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import { Assistant } from "@gram/client/models/components/assistant.js";
import {
  invalidateAllAssistantsList,
  useAssistantsUpdateMutation,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

/**
 * Full-height modal for viewing and editing an assistant's system
 * instructions. The detail panel only shows a clipped preview; this is the
 * place to read the whole prompt and edit it directly.
 */
export function EditInstructionsDialog({
  assistant,
  open,
  onOpenChange,
  onUpdated,
}: {
  assistant: Assistant;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpdated?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");

  const [draft, setDraft] = useState(assistant.instructions ?? "");

  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
      toast.success("System instructions updated");
      onOpenChange(false);
    },
    onError: () => {
      toast.error("Failed to update system instructions");
    },
  });

  // Reset the editor to the latest value on the rising edge of `open` so it
  // never shows a stale draft from a previous cancelled edit. Keyed on the
  // transition (via the ref) rather than every instructions change so a
  // background refetch can't wipe an in-progress edit. The dialog is opened by
  // the parent's `open` prop, which never flows through Radix's onOpenChange,
  // so this can't live in the open-change handler.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (open && !wasOpen.current) setDraft(assistant.instructions ?? "");
    wasOpen.current = open;
  }, [open, assistant.instructions]);

  const dirty = draft !== (assistant.instructions ?? "");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="flex max-h-[85vh] w-[min(90vw,720px)] flex-col gap-4">
        <Dialog.Title>System instructions</Dialog.Title>
        <Dialog.Description>
          The system prompt that governs how {assistant.name} behaves.
        </Dialog.Description>

        <TextArea
          value={draft}
          onChange={setDraft}
          disabled={!canWrite || update.isPending}
          placeholder="Describe how this assistant should behave…"
          className="min-h-[40vh] flex-1 font-mono text-xs"
          rows={20}
        />

        <Dialog.Footer className="items-center">
          {!canWrite && (
            <Type muted small className="mr-auto">
              You don't have permission to edit instructions.
            </Type>
          )}
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={() =>
              update.mutate({
                request: {
                  updateAssistantForm: {
                    id: assistant.id,
                    instructions: draft,
                  },
                },
              })
            }
            disabled={!canWrite || !dirty || update.isPending}
          >
            {update.isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              "Save"
            )}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
