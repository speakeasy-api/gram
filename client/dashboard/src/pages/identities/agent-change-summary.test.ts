import { describe, expect, it } from "vitest";
import {
  agentChangeSummary,
  type AgentChangeNames,
} from "./agent-change-summary";

const names: AgentChangeNames = {
  server: (id) => (id === "server-ops" ? "Acme Ops" : undefined),
  project: (id) => (id === "project-default" ? "Default" : undefined),
};

describe("agentChangeSummary", () => {
  it.each([
    {
      case: "a server added to the agent",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "mcp:connect",
          selector: { resource_kind: "mcp", resource_id: "server-ops" },
        },
      },
      want: "mcp:connect · Acme Ops",
    },
    {
      case: "a server removed, from its before snapshot",
      log: {
        action: "agent:policy_grant_delete",
        beforeSnapshot: {
          scope: "mcp:connect",
          selector: { resource_kind: "mcp", resource_id: "server-ops" },
        },
      },
      want: "mcp:connect · Acme Ops",
    },
    {
      case: "a grant on every server",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "mcp:connect",
          selector: { resource_kind: "mcp", resource_id: "*" },
        },
      },
      want: "mcp:connect · every server",
    },
    {
      case: "a grant on every server in one project",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "mcp:connect",
          selector: {
            resource_kind: "mcp",
            resource_id: "*",
            project_id: "project-default",
          },
        },
      },
      want: "mcp:connect · every server in Default",
    },
    {
      case: "a grant on every upstream credential in an unknown project",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "mcp:auth",
          selector: {
            resource_kind: "mcp_auth_client",
            resource_id: "*",
            project_id: "0123456789abcdef",
          },
        },
      },
      want: "mcp:auth · every upstream credential in project 01234567…",
    },
    {
      case: "an unknown server by its short id",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "mcp:connect",
          selector: {
            resource_kind: "mcp",
            resource_id: "0123456789abcdef",
          },
        },
      },
      want: "mcp:connect · server 01234567…",
    },
    {
      case: "a project grant",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "project:read",
          selector: {
            resource_kind: "project",
            resource_id: "project-default",
          },
        },
      },
      want: "project:read · Default",
    },
    {
      case: "an organization grant",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "org:hooks_ingest",
          selector: { resource_kind: "org", resource_id: "org-id" },
        },
      },
      want: "org:hooks_ingest · the organization",
    },
    {
      case: "a grant on every upstream credential",
      log: {
        action: "agent:policy_grant_create",
        afterSnapshot: {
          scope: "mcp:auth",
          selector: { resource_kind: "mcp_auth_client", resource_id: "*" },
        },
      },
      want: "mcp:auth · every upstream credential",
    },
    {
      case: "a connection with its upstream account",
      log: {
        action: "agent:connection_attach",
        metadata: { upstream_account: "ada@example.com" },
      },
      want: "Signs in as ada@example.com",
    },
  ])("describes $case", ({ log, want }) => {
    expect(agentChangeSummary(log, names)).toBe(want);
  });

  it.each([
    { action: "agent:rename" },
    { action: "agent:connection_detach", metadata: {} },
    { action: "agent:policy_grant_create", afterSnapshot: {} },
  ])("has nothing to add for $action", (log) => {
    expect(agentChangeSummary(log, names)).toBeUndefined();
  });
});
