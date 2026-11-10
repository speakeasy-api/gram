import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Row } from "./PanelSection";

export function AssistantRuntimeSettings({
  assistant,
  canWrite,
  onUpdated,
}: {
  assistant: Assistant;
  canWrite: boolean;
  onUpdated?: () => void;
}): JSX.Element {
  const flag = useFeatureFlag(FEATURE_FLAGS.assistantKitRuntime);
  const queryClient = useQueryClient();
  const update = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
    },
    onError: (error) => {
      toast.error(
        error.message ||
          "Unable to switch runtime. Wait for all assistant work to finish and try again.",
      );
    },
  });
  const isKit = assistant.runtimeVersion === 3;
  const knownVersion = isKit || assistant.runtimeVersion === 2;

  return (
    <>
      <Row label="Runtime">
        <Text small>
          {isKit
            ? "Kit"
            : assistant.runtimeVersion === 2
              ? "Default"
              : "Unavailable"}
        </Text>
      </Row>
      {flag.status === "enabled" && (
        <Row label="Use Kit runtime">
          <Switch
            aria-label="Use Kit runtime"
            checked={isKit}
            disabled={!canWrite || !knownVersion || update.isPending}
            onCheckedChange={(checked) => {
              if (!canWrite || !knownVersion || update.isPending) return;
              update.mutate({
                request: {
                  updateAssistantForm: {
                    id: assistant.id,
                    runtimeVersion: checked ? 3 : 2,
                    expectedRuntimeVersion: isKit ? 3 : 2,
                  },
                },
              });
            }}
          />
        </Row>
      )}
    </>
  );
}
