import {
  claudeCodeAddCommand,
  codexAddCommand,
  cursorInstallLink,
} from "@/lib/mcp-client-install";
import { defineResource, type Status } from "../kit/types";
import {
  AVAILABILITY_LABEL,
  GATEWAY_PREFIX,
  isGatewayId,
  SOURCE_LABEL,
  type McpServerAvailability,
  type McpServerRecord,
  type McpServerRow,
} from "./model";

const AVAILABILITY_TONE: Record<McpServerAvailability, Status["tone"]> = {
  disabled: "neutral",
  private: "success",
  public: "warning",
};

/** Matches the existing MCP settings pages. */
const NAME_MAX_LENGTH = 40;

function availabilityStatus(row: McpServerRow): Status {
  return {
    label: AVAILABILITY_LABEL[row.availability],
    tone: AVAILABILITY_TONE[row.availability],
  };
}

/** Pulls an MCP server reference out of a Platform MCP tool call. */
function mcpReference(input: unknown, output: unknown): string | undefined {
  const pick = (value: unknown, keys: string[]): string | undefined => {
    if (value == null || typeof value !== "object") return undefined;
    const record = value as Record<string, unknown>;
    for (const key of keys) {
      const candidate = record[key];
      if (typeof candidate === "string" && candidate) return candidate;
    }
    for (const nested of ["mcp", "server", "gateway", "result"]) {
      const found = pick(record[nested], keys);
      if (found) return found;
    }
    return undefined;
  };
  const gateway = pick(input, ["gateway_id"]) ?? pick(output, ["gateway_id"]);
  if (gateway) return GATEWAY_PREFIX + gateway;
  return (
    pick(input, ["mcp_id", "mcp_server_id", "mcp_slug"]) ??
    pick(output, ["mcp_id", "mcp_server_id", "id", "slug"])
  );
}

export const mcpServersDefinition = defineResource<
  McpServerRow,
  McpServerRecord
>({
  id: "mcp-servers",
  noun: { one: "MCP server", many: "MCP servers" },
  area: "Connect",

  list: {
    title: "MCP servers",
    description:
      "The servers your team's agents can reach, and what each one is built from.",
    rowId: (row) => row.id,
    searchText: (row) => `${row.name} ${row.summary}`,
    columns: [
      {
        id: "name",
        header: "Server",
        kind: "identity",
        value: (row) => ({
          mark: row.mark,
          title: row.name,
          subtitle: row.summary,
        }),
      },
      {
        id: "source",
        header: "Type",
        kind: "value",
        value: (row) => ({
          kind: "tag",
          text: SOURCE_LABEL[row.source],
          tone: row.source === "gateway" ? "information" : "neutral",
        }),
      },
      {
        id: "tools",
        header: "Tools",
        kind: "value",
        align: "end",
        value: (row) =>
          row.toolCount == null
            ? { kind: "empty", text: "—" }
            : { kind: "number", value: row.toolCount },
      },
      {
        id: "availability",
        header: "Status",
        kind: "value",
        value: (row) => ({ kind: "status", status: availabilityStatus(row) }),
      },
    ],
    filters: [
      {
        id: "type",
        label: "Type",
        options: [
          { value: "servers", label: "Servers" },
          { value: "gateways", label: "Gateways" },
        ],
        matches: (row, value) =>
          value === "gateways"
            ? row.source === "gateway"
            : row.source !== "gateway",
      },
    ],
    primaryAction: "create",
    empty: {
      icon: "server",
      heading: "Connect your first MCP server",
      description:
        "Pick one from the catalog, point at a remote URL, or build one from an API. Your team's agents can use it as soon as it's on.",
    },
  },

  record: {
    header: (rec) => ({
      kicker:
        rec.source === "gateway"
          ? "MCP gateway"
          : `MCP server · ${SOURCE_LABEL[rec.source]}`,
      identity: { mark: rec.mark, title: rec.name },
      description: rec.summary,
      status: availabilityStatus(rec),
    }),
    secondaryActions: ["ask"],
    sections: [
      {
        id: "next",
        kind: "checklist",
        title: "Get it to your team",
        hideWhenDone: true,
        steps: (rec) => [
          {
            id: "on",
            title: "Turn it on",
            description: "Agents can only reach servers that are on.",
            done: rec.availability !== "disabled",
            action: "turnOn",
          },
          {
            id: "distribute",
            title: "Distribute it as a plugin",
            description:
              "Everyone gets it in Claude and Claude Code, with no URL to pass around.",
            done: rec.distributed,
            action: "distribute",
          },
          {
            id: "try",
            title: "Try it",
            description: "Ask the assistant something that needs this server.",
            done: rec.used,
            action: "tryIt",
          },
        ],
      },
      {
        id: "facts",
        kind: "facts",
        facts: [
          {
            label: "Tools",
            value: (rec) =>
              rec.toolCount == null
                ? { kind: "empty", text: "Found on first connect" }
                : { kind: "number", value: rec.toolCount },
          },
          {
            label: "Type",
            value: (rec) => ({ kind: "text", text: SOURCE_LABEL[rec.source] }),
          },
          {
            label: "Plugin",
            value: (rec) =>
              rec.distributed
                ? { kind: "text", text: "Distributed" }
                : { kind: "empty", text: "Not distributed" },
          },
          {
            label: "Created",
            value: (rec) => ({ kind: "date", date: rec.createdAt }),
          },
        ],
      },
      {
        id: "connect",
        kind: "code",
        title: "Connect it yourself",
        description:
          "Add it to your client and sign in with your Speakeasy account when it asks.",
        // A command without a real URL would break when copied.
        when: (rec) => rec.url != null,
        samples: (rec) => {
          const url = rec.url ?? "";
          return [
            {
              id: "claude",
              label: "Claude Code",
              code: claudeCodeAddCommand(rec.name, url, { multiline: true }),
            },
            {
              id: "codex",
              label: "Codex",
              code: codexAddCommand(rec.name, url, { multiline: true }),
            },
            {
              id: "cursor",
              label: "Cursor",
              code: cursorInstallLink(rec.name, url),
            },
          ];
        },
      },
      {
        id: "tools",
        kind: "list",
        when: (rec) => !isGatewayId(rec.id),
        title: "Tools",
        empty:
          "No tools yet. Remote servers list their tools after the first connection.",
        items: (rec) =>
          rec.tools.slice(0, 8).map((tool) => ({
            id: tool.name,
            title: tool.name,
            description: tool.description,
            mono: true,
          })),
        total: (rec) => rec.toolCount ?? rec.tools.length,
      },
      {
        id: "members",
        kind: "list",
        when: (rec) => isGatewayId(rec.id),
        title: "Servers behind it",
        empty: "This gateway doesn't front any servers yet.",
        items: (rec) =>
          rec.members.map((m) => ({
            id: m.id,
            title: m.name,
            opens: { resource: "mcp-servers", id: m.id },
          })),
      },
      {
        id: "settings",
        kind: "settings",
        title: "Settings",
        fields: [
          {
            id: "name",
            label: "Name",
            description: "Shown to people and agents choosing a server.",
            kind: "text",
            value: (rec) => rec.name,
            action: "rename",
            validate: (v) =>
              !v.trim()
                ? "Give it a name."
                : v.trim().length > NAME_MAX_LENGTH
                  ? `Keep it to ${NAME_MAX_LENGTH} characters or fewer.`
                  : undefined,
          },
          {
            id: "availability",
            label: "Availability",
            kind: "choice",
            value: (rec) => rec.availability,
            options: (rec) => [
              {
                value: "private",
                label: AVAILABILITY_LABEL.private,
                description: "People and agents with access can connect.",
              },
              {
                value: "disabled",
                label: AVAILABILITY_LABEL.disabled,
                description: "Offline. Nobody can connect.",
              },
              // Hosted servers can go public. Tunneled ones need the tunnel
              // owner's consent first, which the server's settings page
              // collects; one that is already public keeps that choice here.
              ...(rec.source === "hosted" ||
              (rec.source === "tunneled" && rec.availability === "public")
                ? [
                    {
                      value: "public",
                      label: AVAILABILITY_LABEL.public,
                      description:
                        "Anyone with the URL can read its tools. Using them still needs sign-in.",
                    },
                  ]
                : []),
            ],
            action: "setAvailability",
          },
        ],
      },
    ],
    dangerActions: ["delete"],
  },

  actions: [
    {
      id: "create",
      label: "New server",
      kind: "chat",
      scope: "mcp:write",
      prompt: "I want to connect a new MCP server. What are my options?",
      sdk: [
        "useRemoteMcpInstallWorkflow",
        "client.metaMcp.create",
        "client.toolsets.create",
      ],
    },
    {
      id: "turnOn",
      label: "Turn on",
      kind: "run",
      scope: "mcp:write",
      sdk: ["useMcpServerVisibilityUpdate", "useUpdateMcpServerMutation"],
    },
    {
      id: "setAvailability",
      label: "Change availability",
      kind: "set",
      scope: "mcp:write",
      sdk: ["useMcpServerVisibilityUpdate", "useUpdateMcpServerMutation"],
    },
    {
      id: "rename",
      label: "Rename",
      kind: "set",
      scope: "mcp:write",
      sdk: ["useUpdateMcpServerMutation", "useUpdateMetaMcpServerMutation"],
    },
    {
      id: "distribute",
      label: "Distribute as plugin",
      kind: "chat",
      scope: "plugin:write",
      prompt: "Distribute {title} as a plugin to my whole team.",
      sdk: ["useAddPluginServerMutation", "usePublishPluginsMutation"],
    },
    {
      id: "tryIt",
      label: "Try it",
      kind: "chat",
      prompt:
        "Use {title} to show me what it can do. Pick one useful tool and run it.",
      sdk: [],
    },
    {
      id: "ask",
      label: "Ask about this",
      kind: "chat",
      prompt:
        "Tell me about the {title} MCP server: who uses it and whether anything needs attention.",
      sdk: [],
    },
    {
      id: "delete",
      label: "Delete this server",
      kind: "confirm",
      danger: true,
      leavesRecord: true,
      scope: "mcp:write",
      confirm: {
        title: "Delete this server?",
        description:
          "Agents lose its tools at once, and plugins that include it stop offering it. This can't be undone.",
        confirmLabel: "Delete server",
        pendingLabel: "Deleting…",
      },
      sdk: ["useDeleteMcpServerMutation", "useDeleteMetaMcpServerMutation"],
    },
  ],

  chat: {
    tools: {
      find_mcp: { opens: "list" },
      list_mcps: { opens: "list" },
      list_my_servers: { opens: "list" },
      get_mcp: { opens: "record", id: mcpReference },
      get_mcp_readiness: { opens: "record", id: mcpReference },
      get_mcp_diagnostics: { opens: "record", id: mcpReference },
      enable_mcp: { opens: "record", id: mcpReference },
      disable_mcp: { opens: "record", id: mcpReference },
      update_mcp_metadata: { opens: "record", id: mcpReference },
      register_remote_mcp: { opens: "record", id: mcpReference },
      register_catalog_mcp: { opens: "record", id: mcpReference },
      create_gateway: { opens: "record", id: mcpReference },
      set_gateway_members: { opens: "record", id: mcpReference },
    },
    keywords: ["server", "servers", "gateway", "gateways", "our", "existing"],
    suggestions: [
      "Show me all our MCP servers",
      "Which servers are off?",
      "Connect Linear from the catalog",
    ],
  },
});
