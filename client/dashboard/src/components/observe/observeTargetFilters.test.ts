import { describe, expect, it } from "vitest";
import {
  buildServerOptionGroups,
  encodeGatewayServerFilter,
  parseTargetFilter,
  selectedUserAgentIds,
  selectedUserEmails,
  selectedUserFilterValues,
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
