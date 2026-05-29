import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Tool } from "@/lib/toolTypes";
import { Confirm } from "@gram/client/models/components";
import { invalidateTemplate } from "@gram/client/react-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

export type ToolUpdateFields = {
  name?: string;
  description?: string;
  title?: string;
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
  // tri-state:
  //   undefined = no override (use source tags)
  //   []        = explicit empty override
  //   [...]     = explicit override
  tags?: string[] | undefined;
};

export type UseToolUpdateOptions = {
  /** Telemetry event name to capture on success (e.g. "toolset_event", "source_event"). */
  telemetryEvent: string;
  /** Called after a successful update so the caller can refresh its view. */
  onSuccess?: () => void;
};

/**
 * Shared mutation for updating a tool's overrides. Routes prompt-template tools
 * through `templates.update` and all other tool types through the global tool
 * variation upsert. Returns a stable `updateTool` callback plus an `isUpdating`
 * flag callers can wire into Save-button disabled state to prevent
 * double-submission.
 */
export function useToolUpdate({
  telemetryEvent,
  onSuccess,
}: UseToolUpdateOptions) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const client = useSdkClient();

  const { mutateAsync, isPending } = useMutation<
    void,
    Error,
    { tool: Tool; updates: ToolUpdateFields }
  >({
    mutationFn: async ({ tool, updates }) => {
      if (tool.type === "prompt") {
        await client.templates.update({
          updatePromptTemplateForm: {
            ...tool,
            ...updates,
          },
        });
        invalidateTemplate(queryClient, [{ name: tool.name }]);
      } else {
        await client.variations.upsertGlobal({
          upsertGlobalToolVariationForm: {
            ...tool.variation,
            confirm: tool.variation?.confirm as Confirm,
            ...updates,
            srcToolName: tool.canonicalName,
            srcToolUrn: tool.toolUrn,
          },
        });
      }

      telemetry.capture(telemetryEvent, {
        action: "tool_variation_updated",
        tool_name: tool.name,
        overridden_fields: Object.keys(updates).join(", "),
      });
    },
    onSuccess: () => {
      toast.success("Tool updated");
      onSuccess?.();
    },
    onError: (err) => {
      toast.error(`Failed to update tool: ${err.message}`);
    },
  });

  const updateTool = useCallback(
    (tool: Tool, updates: ToolUpdateFields) => mutateAsync({ tool, updates }),
    [mutateAsync],
  );

  return { updateTool, isUpdating: isPending };
}
