import { describe, expect, it } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MemberRow } from "./memberRows";
import {
  discoveredWithoutExecuting,
  memberUsageRows,
  metaToolUsageItems,
} from "./gatewayActivity";

function row(
  mcpServerId: string,
  server: Partial<McpServer> | undefined,
  memberName?: string,
): MemberRow {
  return {
    member: {
      id: `member-${mcpServerId}`,
      mcpServerId,
      mcpServerName: memberName,
      sortOrder: 0,
    },
    server: server ? ({ id: mcpServerId, ...server } as McpServer) : undefined,
    classification: server ? "hosted" : "unknown",
  };
}

describe("metaToolUsageItems", () => {
  it("orders the tools the way an agent reaches for them", () => {
    const items = metaToolUsageItems({
      listServers: 10,
      describeServer: 6,
      describeTools: 4,
      executeTool: 3,
    });
    expect(items.map((i) => i.key)).toEqual([
      "list_servers",
      "describe_server",
      "describe_tools",
      "execute_tool",
    ]);
    expect(items.map((i) => i.value)).toEqual([10, 6, 4, 3]);
  });
});

describe("memberUsageRows", () => {
  it("labels members by server name, then member name, then slug, then id", () => {
    const rows = memberUsageRows(
      [
        { mcpServerId: "a", toolCalls: 2, errorCount: 1 },
        { mcpServerId: "b", toolCalls: 1, errorCount: 0 },
        { mcpServerId: "c", toolCalls: 1, errorCount: 0 },
        { mcpServerId: "gone", toolCalls: 5, errorCount: 5 },
      ],
      [
        row("a", { name: "Alpha", slug: "alpha" }),
        row("b", { slug: "beta" }, "Beta member"),
        row("c", { slug: "gamma" }),
      ],
    );
    expect(rows.map((r) => r.label)).toEqual([
      "Alpha",
      "Beta member",
      "gamma",
      "gone",
    ]);
  });

  it("computes the error rate and keeps a zero-call member at 0%", () => {
    const rows = memberUsageRows(
      [
        { mcpServerId: "a", toolCalls: 4, errorCount: 1 },
        { mcpServerId: "b", toolCalls: 0, errorCount: 0 },
      ],
      [],
    );
    expect(rows[0]?.errorRate).toBe(25);
    expect(rows[1]?.errorRate).toBe(0);
  });
});

describe("discoveredWithoutExecuting", () => {
  it("is true when discovery ran but execute_tool never did", () => {
    expect(
      discoveredWithoutExecuting({
        listServers: 1,
        describeServer: 3,
        describeTools: 0,
        executeTool: 0,
      }),
    ).toBe(true);
  });

  it("is false with no activity, with executions, or without data", () => {
    expect(
      discoveredWithoutExecuting({
        listServers: 0,
        describeServer: 0,
        describeTools: 0,
        executeTool: 0,
      }),
    ).toBe(false);
    expect(
      discoveredWithoutExecuting({
        listServers: 1,
        describeServer: 1,
        describeTools: 1,
        executeTool: 2,
      }),
    ).toBe(false);
    expect(discoveredWithoutExecuting(undefined)).toBe(false);
  });
});
