import { type ToolCallMessagePartComponent } from "@assistant-ui/react";
import { useToolApproval } from "@/elements/hooks/useToolApproval";
import {
  ToolUI,
  type ToolStatus,
  type ContentItem,
} from "@/elements/components/ui/tool-ui";

export const ToolFallback: ToolCallMessagePartComponent = ({
  toolName,
  toolCallId,
  status,
  result,
  args,
}) => {
  const {
    pendingApprovals,
    whitelistTool,
    confirmPendingApproval,
    rejectPendingApproval,
  } = useToolApproval();

  // Check if this specific tool call has a pending approval
  const pendingApproval = pendingApprovals.get(toolCallId);

  const handleApproveOnce = () => {
    confirmPendingApproval(toolCallId);
  };

  const handleApproveForSession = () => {
    whitelistTool(toolName);
    confirmPendingApproval(toolCallId);
  };

  const handleDeny = () => {
    rejectPendingApproval(toolCallId);
  };

  // Map assistant-ui status to ToolUI status
  const getToolStatus = (): ToolStatus => {
    if (pendingApproval) return "approval";
    if (status.type === "incomplete") return "error";
    if (status.type === "complete") {
      // Check if the result indicates an error (e.g., tool was denied)
      if (
        result &&
        typeof result === "object" &&
        "isError" in result &&
        result.isError
      ) {
        return "error";
      }
      return "complete";
    }
    return "running";
  };

  // Parse result to structured content if possible
  const getResult = ():
    | string
    | Record<string, unknown>
    | { content: ContentItem[] }
    | undefined => {
    if (result === undefined) return undefined;
    // Check if it's structured content with a content array
    if (
      typeof result === "object" &&
      result !== null &&
      "content" in result &&
      Array.isArray((result as { content: unknown }).content)
    ) {
      return result as { content: ContentItem[] };
    }
    // Otherwise return as-is (string or object)
    if (typeof result === "string") return result;
    return result as Record<string, unknown>;
  };

  return (
    // Width follows the call's own content: a collapsed call is a short label,
    // and stretching it to the message column leaves the chevron marooned at
    // the far edge. Expanding (arguments, output) grows the card as needed.
    <div className="aui-tool-fallback-root flex w-fit max-w-full flex-col">
      <ToolUI
        name={toolName}
        status={getToolStatus()}
        request={args as Record<string, unknown>}
        result={getResult()}
        onApproveOnce={pendingApproval ? handleApproveOnce : undefined}
        onApproveForSession={
          pendingApproval ? handleApproveForSession : undefined
        }
        onDeny={pendingApproval ? handleDeny : undefined}
      />
    </div>
  );
};
