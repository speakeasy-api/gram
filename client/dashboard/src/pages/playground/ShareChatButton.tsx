import { useTelemetry } from "@/contexts/Telemetry";
import { ShareButton, useThreadId } from "@gram-ai/elements";
import { toast } from "sonner";

/**
 * Playground-specific share button that wraps the Elements ShareButton
 * with telemetry tracking and toast notifications.
 */
export function ShareChatButton() {
  const telemetry = useTelemetry();
  const { threadId } = useThreadId();

  return (
    <ShareButton
      onShare={(result) => {
        if ("url" in result) {
          // Track successful share
          telemetry.capture("chat_event", {
            action: "chat_shared",
            thread_id: threadId,
          });
          toast.success("Chat link copied to clipboard");
        } else {
          toast.error(result.error.message);
        }
      }}
    />
  );
}
