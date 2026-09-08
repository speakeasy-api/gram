import { describe, expect, it } from "vitest";

import { platformMCPMarketplaceRepoURL } from "./platform-mcp-marketplace";

describe("platformMCPMarketplaceRepoURL", () => {
  it("uses the local Git marketplace in development", () => {
    expect(platformMCPMarketplaceRepoURL(true, "https://localhost:8080/")).toBe(
      "https://localhost:8080/marketplace/local-platform-mcp-marketplace-000000000000.git",
    );
  });

  it("uses the public marketplace outside development", () => {
    expect(platformMCPMarketplaceRepoURL(false, "https://localhost:8080")).toBe(
      "https://github.com/speakeasy-api/marketplace",
    );
  });
});
