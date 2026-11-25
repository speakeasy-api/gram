import { fixtureData, type FixtureState } from "../kit/fixtures";
import type { ResourceData } from "../kit/types";
import { mcpServersDefinition } from "./definition";
import {
  GATEWAY_PREFIX,
  type McpServerAvailability,
  type McpServerRecord,
  type McpServerRow,
} from "./model";

const day = 24 * 60 * 60 * 1000;
const NOW = new Date("2026-11-20T10:00:00Z").getTime();
const ago = (ms: number) => new Date(NOW - ms);

const LINEAR_TOOLS = [
  ["list_issues", "Find issues by assignee, state, label or cycle"],
  ["create_issue", "Open an issue in any team"],
  ["update_issue", "Change state, assignee, priority or estimate"],
  ["add_comment", "Comment on an issue as the signed-in user"],
  ["list_projects", "Projects with their lead, status and target date"],
  ["get_cycle", "The active cycle for a team and its scope"],
  ["search_documents", "Full-text search across Linear docs"],
  ["list_teams", "Teams you belong to"],
  ["get_user", "A person's profile and their open issues"],
].map(([name, description]) => ({ name: name!, description }));

function server(
  partial: Partial<McpServerRecord> &
    Pick<McpServerRecord, "id" | "name" | "source" | "summary">,
): McpServerRecord {
  return {
    // The backend id, as the SDK adapter produces it: no `gateway:` prefix.
    serverId: partial.id.replace(GATEWAY_PREFIX, ""),
    availability: "private",
    toolCount: partial.tools?.length,
    mark: { label: partial.name },
    updatedAt: ago(2 * day),
    url: `https://app.speakeasy.com/mcp/${partial.name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`,
    tools: [],
    members: [],
    distributed: true,
    used: true,
    createdAt: ago(40 * day),
    ...partial,
  };
}

const MCP_SERVER_RECORDS: McpServerRecord[] = [
  server({
    id: "srv-linear",
    name: "Linear",
    source: "remote",
    summary: "Issues, projects and cycles",
    tools: LINEAR_TOOLS,
    toolCount: 24,
    distributed: false,
    used: false,
    createdAt: ago(60 * 1000),
    updatedAt: ago(60 * 1000),
  }),
  server({
    id: "gateway:project",
    name: "Project gateway",
    source: "gateway",
    summary: "Every server in this project, at one URL",
    toolCount: 142,
    mark: { label: "Speakeasy" },
    members: [
      { id: "srv-linear", name: "Linear" },
      { id: "srv-datadog", name: "Datadog" },
      { id: "srv-notion", name: "Notion" },
    ],
  }),
  server({
    id: "gateway:engineering",
    name: "Engineering",
    source: "gateway",
    summary: "Fronts Datadog and Notion at one URL",
    toolCount: 97,
    mark: { label: "Datadog", secondary: { label: "Notion" } },
    members: [
      { id: "srv-datadog", name: "Datadog" },
      { id: "srv-notion", name: "Notion" },
    ],
  }),
  server({
    id: "srv-datadog",
    name: "Datadog",
    source: "hosted",
    summary: "Logs, metrics and monitors",
    tools: [
      { name: "search_logs", description: "Query logs with Datadog syntax" },
      { name: "get_monitor", description: "A monitor's state and history" },
    ],
    toolCount: 41,
  }),
  server({
    id: "srv-notion",
    name: "Notion",
    source: "remote",
    summary: "Pages and databases, read-only",
    toolCount: undefined,
    tools: [],
    availability: "disabled",
    distributed: false,
    used: false,
  }),
  server({
    id: "srv-internal",
    name: "Internal billing API (staging, read-only replica for finance reporting)",
    source: "tunneled",
    summary:
      "Built from the billing-service OpenAPI document behind the private network tunnel",
    toolCount: 7,
    tools: [
      {
        name: "get_invoice_by_external_reference_identifier",
        description:
          "Looks up a single invoice using the identifier the upstream payment processor assigned to it",
      },
    ],
  }),
];

const toRow = ({
  id,
  serverId,
  name,
  source,
  summary,
  availability,
  toolCount,
  mark,
  updatedAt,
}: McpServerRecord): McpServerRow => ({
  id,
  serverId,
  name,
  source,
  summary,
  availability,
  toolCount,
  mark,
  updatedAt,
});

export function mcpServersFixtureData({
  state = "ready",
  readOnly = false,
}: { state?: FixtureState; readOnly?: boolean } = {}): ResourceData<
  McpServerRow,
  McpServerRecord
> {
  return fixtureData({
    definition: mcpServersDefinition,
    rows: MCP_SERVER_RECORDS.map(toRow),
    records: Object.fromEntries(MCP_SERVER_RECORDS.map((r) => [r.id, r])),
    state,
    readOnly,
    apply: (rec, action, input) => {
      switch (action) {
        case "turnOn":
          return { ...rec, availability: "private" };
        case "setAvailability":
          return { ...rec, availability: input as McpServerAvailability };
        case "rename":
          return { ...rec, name: input ?? rec.name };
        case "distribute":
          return { ...rec, distributed: true };
        case "tryIt":
          return { ...rec, used: true };
        default:
          return rec;
      }
    },
  });
}
