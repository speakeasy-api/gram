import { Button } from "@/components/ui/Button";
import type { useToolsetMcpTarget } from "@/hooks/useToolsetUrl";
import { Text } from "@/components/ui/Text";

type PendingStatus = Exclude<
  ReturnType<typeof useToolsetMcpTarget>["status"],
  "ready"
>;

const messages: Record<PendingStatus, string> = {
  idle: "Select an MCP server to view authentication and sessions.",
  loading: "Loading MCP server authentication…",
  error: "Unable to load MCP server authentication.",
  unavailable:
    "This MCP server is disabled. Enable it to view authentication and sessions.",
};

export function ToolsetMcpTargetStatus({
  status,
  onRetry,
}: {
  status: PendingStatus;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <div
      className="py-4 text-center"
      role={status === "error" ? "alert" : "status"}
    >
      <Text variant="small" className="text-muted-foreground">
        {messages[status]}
      </Text>
      {status === "error" && onRetry && (
        <Button size="sm" variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  );
}
