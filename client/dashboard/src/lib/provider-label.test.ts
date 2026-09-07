import { describe, expect, it } from "vitest";

import { providerLabel } from "@/lib/provider-label";

describe("providerLabel", () => {
  it("drops the service prefix and TLD that carry no meaning", () => {
    expect(providerLabel("mcp.linear.app")).toBe("Linear");
    expect(providerLabel("mcp.notion.com")).toBe("Notion");
    expect(providerLabel("auth.atlassian.com")).toBe("Atlassian");
  });

  it("trims the api suffix that reads as plumbing", () => {
    expect(providerLabel("oauth.googleapis.com")).toBe("Google");
  });

  it("keeps a bare host that has nothing to strip", () => {
    expect(providerLabel("supabase.co")).toBe("Supabase");
  });

  it("survives a slug that is entirely prefix", () => {
    // Nothing meaningful is left after filtering, so the host stands in rather
    // than the label collapsing to an empty string.
    expect(providerLabel("mcp.app")).not.toBe("");
  });

  it("handles a URL-shaped slug", () => {
    expect(providerLabel("https://mcp.linear.app/mcp")).toBe("Linear");
  });
});
