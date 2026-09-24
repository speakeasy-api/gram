import type { SetupWorkstream } from "@gram/client/models/components/setupworkstream.js";

// Representative API catalog for tests; production membership comes from the server.
export const SETUP_WORKSTREAMS: SetupWorkstream[] = [
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
];
