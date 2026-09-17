import { describe, expect, it } from "vitest";

import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import { privateMcpEndpointUrls } from "./usePrivateMcpServerUrls";

const endpoints: McpEndpoint[] = [
  {
    id: "platform-endpoint",
    projectId: "project-1",
    mcpServerId: "server-1",
    slug: "platform-server",
    isDomainRoot: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  },
  {
    id: "custom-endpoint",
    projectId: "project-1",
    mcpServerId: "server-1",
    customDomainId: "domain-1",
    slug: "custom-server",
    isDomainRoot: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  },
];

const onlineIngress = {
  dnsName: "private.example.ts.net",
  endpointNamespaceKind: "platform" as const,
  customDomainId: undefined,
  enabled: true,
  status: "online",
};

describe("privateMcpEndpointUrls", () => {
  it("returns only endpoints in the ingress namespace", () => {
    expect(privateMcpEndpointUrls(onlineIngress, endpoints)).toEqual([
      "https://private.example.ts.net/mcp/platform-server",
    ]);
  });

  it.each([
    { enabled: false, status: "deleting" },
    { enabled: true, status: "pending" },
    { enabled: true, status: "error" },
  ])("does not advertise an unavailable ingress", (state) => {
    expect(
      privateMcpEndpointUrls({ ...onlineIngress, ...state }, endpoints),
    ).toEqual([]);
  });
});
