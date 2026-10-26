import { describe, expect, it } from "vitest";
import {
  buildServerOptionGroups,
  encodeGatewayServerFilter,
  hostedServerFilterValue,
  parseTargetFilter,
  selectedUserAgentIds,
  selectedUserEmails,
  selectedUserFilterValues,
  splitHostedTargets,
} from "./observeTargetFilters";

describe("selectedUserEmails", () => {
  it("normalizes selected email filters", () => {
    expect(
      selectedUserEmails([
        {
          display: "Alice@Example.com, bob@example.com",
          filters: [" Alice@Example.com ", "bob@example.com", ""],
          path: "user.email",
        },
      ]),
    ).toEqual(["alice@example.com", "bob@example.com"]);
  });
});

describe("agent user filters", () => {
  const chips = [
    {
      display: "Alice@Example.com, agent:a1",
      filters: [" Alice@Example.com ", "agent:a1", "agent:"],
      path: "user.email",
    },
  ];

  it("keeps agents out of the email filters", () => {
    expect(selectedUserEmails(chips)).toEqual(["alice@example.com"]);
  });

  it("selects agents by id", () => {
    expect(selectedUserAgentIds(chips)).toEqual(["a1"]);
  });

  it("round-trips both kinds as the URL holds them", () => {
    expect(selectedUserFilterValues(chips)).toEqual([
      "alice@example.com",
      "agent:a1",
    ]);
  });
});

describe("gateway target filters", () => {
  it("round-trips a gateway filter value", () => {
    const value = encodeGatewayServerFilter("gw-1");
    expect(value).toBe("gateway:gw-1");
    expect(parseTargetFilter(value)).toEqual({ type: "gateway", id: "gw-1" });
  });

  it("lists gateways as their own option group, keeping a selected unknown one", () => {
    const groups = buildServerOptionGroups({
      hostedServers: [],
      shadowServers: [],
      gateways: [
        { metaMcpServerId: "gw-1", name: "Acme Gateway", eventCount: 3 },
      ],
      activeFilters: [
        {
          display: "gateway:gw-2",
          filters: ["gateway:gw-2"],
          path: "gram.tool_call.source",
        },
      ],
      serverNameMappings: {
        rawToDisplay: new Map<string, string>(),
      } as unknown as Parameters<
        typeof buildServerOptionGroups
      >[0]["serverNameMappings"],
    });
    expect(groups).toEqual([
      {
        heading: "Gateways",
        options: [
          { value: "gateway:gw-1", label: "Acme Gateway (3)" },
          { value: "gateway:gw-2", label: "gw-2" },
        ],
      },
    ]);
  });
});

describe("hosted target filters", () => {
  const serverId = "0198a3b2-7c4d-7e5f-8a9b-0c1d2e3f4a5b";
  const serverNameMappings = {
    rawToDisplay: new Map<string, string>(),
  } as unknown as Parameters<
    typeof buildServerOptionGroups
  >[0]["serverNameMappings"];

  it("keeps a UUID-shaped legacy slug a slug", () => {
    expect(parseTargetFilter(`hosted:${serverId}`)).toEqual({
      type: "hosted",
      id: serverId,
    });
    expect(parseTargetFilter(`mcp_server:${serverId}`)).toEqual({
      type: "mcp_server",
      id: serverId,
    });
  });

  it("encodes a hosted option by server id", () => {
    expect(
      hostedServerFilterValue(
        { toolsetSlug: "payments", mcpServerId: serverId },
        [],
      ),
    ).toBe(`mcp_server:${serverId}`);
  });

  it("falls back to the slug when the option has no server id", () => {
    expect(hostedServerFilterValue({ toolsetSlug: "payments" }, [])).toBe(
      "hosted:payments",
    );
  });

  it("keeps a selected legacy slug so the option stays selected", () => {
    const groups = buildServerOptionGroups({
      hostedServers: [
        {
          toolsetSlug: "payments",
          toolsetName: "Payments",
          mcpServerId: serverId,
          eventCount: 4,
        },
      ],
      shadowServers: [],
      activeFilters: [
        {
          display: "hosted:payments",
          filters: ["hosted:payments"],
          path: "gram.tool_call.source",
        },
      ],
      serverNameMappings,
    });
    expect(groups).toEqual([
      {
        heading: "Hosted MCP",
        options: [{ value: "hosted:payments", label: "Payments (4)" }],
      },
    ]);
  });

  it("lists an unselected hosted option by server id", () => {
    const groups = buildServerOptionGroups({
      hostedServers: [
        {
          toolsetSlug: "payments",
          toolsetName: "Payments",
          mcpServerId: serverId,
          eventCount: 4,
        },
      ],
      shadowServers: [],
      activeFilters: [],
      serverNameMappings,
    });
    expect(groups[0]?.options).toEqual([
      { value: `mcp_server:${serverId}`, label: "Payments (4)" },
    ]);
  });
});

describe("splitHostedTargets", () => {
  it("sends server ids and legacy slugs to separate fields", () => {
    const id = "0198a3b2-7c4d-7e5f-8a9b-0c1d2e3f4a5b";
    expect(
      splitHostedTargets([
        { type: "mcp_server", id },
        { type: "hosted", id: "payments" },
        { type: "shadow", id: "db" },
        { type: "gateway", id: "gw-1" },
      ]),
    ).toEqual({ mcpServerIds: [id], hostedToolsetSlugs: ["payments"] });
  });
});
