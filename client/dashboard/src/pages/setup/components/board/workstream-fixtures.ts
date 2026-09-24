import { resolveWorkstreams } from "./tasks";

// Representative API catalog for board tests; production membership comes from the server.
export const ONBOARDING_WORKSTREAMS = resolveWorkstreams([
  {
    id: "connect",
    title: "Connect identity",
    taskKeys: [
      "domain-verification",
      "identity-provider",
      "connect-idp",
      "directory-sync",
    ],
  },
  {
    id: "observe",
    title: "Observe agents",
    taskKeys: [
      "enable-logging",
      "anthropic-observability",
      "instrument-agents",
      "litellm",
      "additional-agent-config",
      "confirm-traffic",
    ],
  },
  {
    id: "distribute",
    title: "MCP Gateway",
    taskKeys: ["create-marketplace", "distribute-servers", "platform-mcp"],
  },
  {
    id: "secure",
    title: "Secure agent traffic",
    taskKeys: ["anthropic-admin-controls", "configure-policies"],
  },
]);
