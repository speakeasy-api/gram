import { createFileRoute } from "@tanstack/react-router";
import { PlatformMcpFeedbackPage } from "@/pages/PlatformMcpFeedback";

export const Route = createFileRoute("/platform-mcp-feedback")({
  component: PlatformMcpFeedbackPage,
  staticData: { crumb: "Platform MCP feedback" },
});
