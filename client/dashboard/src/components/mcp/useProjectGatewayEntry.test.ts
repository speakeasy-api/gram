// @vitest-environment node
import { describe, expect, it } from "vitest";
import { projectGatewayUrl } from "./useProjectGatewayEntry";

describe("projectGatewayUrl", () => {
  it("serves a project beneath its organization's gateway", () => {
    expect(
      projectGatewayUrl("https://app.example.test/mcp/gateway/acme", "sales"),
    ).toBe("https://app.example.test/mcp/gateway/acme/projects/sales");
  });

  it("does not double a trailing slash", () => {
    expect(
      projectGatewayUrl("https://app.example.test/mcp/gateway/acme/", "sales"),
    ).toBe("https://app.example.test/mcp/gateway/acme/projects/sales");
  });
});
