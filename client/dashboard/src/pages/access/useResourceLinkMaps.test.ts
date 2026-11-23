// @vitest-environment node
import { describe, expect, it } from "vitest";

import { hostingServersByToolset } from "./useResourceLinkMaps";

describe("hostingServersByToolset", () => {
  it("picks the server that stands in for each toolset", () => {
    const hosts = hostingServersByToolset([
      // Another server over the same toolset, which is not its host.
      {
        id: "server-alias",
        slug: "crm-alias",
        toolsetId: "toolset-crm",
        toolset: { hostingMcpServerId: "server-crm" },
      },
      {
        id: "server-crm",
        slug: "crm",
        toolsetId: "toolset-crm",
        toolset: { hostingMcpServerId: "server-crm" },
      },
      // A remote server serves no toolset.
      { id: "server-remote", slug: "remote" },
    ]);
    expect([...hosts]).toEqual([
      ["toolset-crm", { id: "server-crm", slug: "crm" }],
    ]);
  });

  it("falls back to the canonical wrapper when the host is not named", () => {
    const hosts = hostingServersByToolset([
      { id: "toolset-ops", slug: "ops", toolsetId: "toolset-ops" },
    ]);
    expect(hosts.get("toolset-ops")).toEqual({
      id: "toolset-ops",
      slug: "ops",
    });
  });
});
