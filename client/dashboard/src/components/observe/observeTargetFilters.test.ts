import { describe, expect, it } from "vitest";
import {
  buildServerOptionGroups,
  encodeGatewayServerFilter,
  parseTargetFilter,
  selectedUserEmails,
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
