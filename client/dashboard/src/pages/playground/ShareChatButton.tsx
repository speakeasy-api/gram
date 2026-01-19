import { Button } from "@/components/ui/button";
import { useTelemetry } from "@/contexts/Telemetry";
import { useThreadId } from "@gram-ai/elements";
import { Link } from "lucide-react";
import { toast } from "sonner";

/**
 * Button component that copies the current chat URL to clipboard for sharing.
 * Uses the Elements thread ID for proper chat sharing integration.
 * Captures telemetry event when chat is shared.
 */
export function ShareChatButton() {
  const { threadId } = useThreadId();
  const telemetry = useTelemetry();

  const handleShare = () => {
    if (!threadId) {
      toast.error("No chat to share yet. Send a message first.");
      return;
    }

    telemetry.capture("chat_event", {
      action: "chat_shared",
      thread_id: threadId,
    });

    // Build share URL with threadId parameter
    const url = new URL(window.location.href);
    url.searchParams.set("threadId", threadId);
    navigator.clipboard.writeText(url.toString());
    toast.success("Chat link copied to clipboard");
  };

  return (
    <Button
      size="sm"
      variant="ghost"
      onClick={handleShare}
      disabled={!threadId}
    >
      <Link className="size-4 mr-2" />
      Share chat
    </Button>
  );
}
