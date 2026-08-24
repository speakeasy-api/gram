import { describe, expect, it } from "vitest";
import {
  activeTabFromPath,
  GATEWAY_TAB_URLS,
  initialTabFromHash,
} from "./GatewayDetailsRouting";

const ID = "00000000-0000-4000-8000-00000000ffff";
const BASE = `/acme/projects/demo/mcp/gateway/${ID}`;

describe("activeTabFromPath", () => {
  it("resolves each declared tab", () => {
    for (const tab of GATEWAY_TAB_URLS) {
      expect(activeTabFromPath(`${BASE}/${tab}`, ID)).toBe(tab);
    }
  });

  it("returns undefined for the bare detail path and unknown tabs", () => {
    expect(activeTabFromPath(BASE, ID)).toBeUndefined();
    expect(activeTabFromPath(`${BASE}/performance`, ID)).toBeUndefined();
  });

  it("requires the mcp/gateway prefix, so a lookalike path misses", () => {
    expect(
      activeTabFromPath(`/acme/projects/demo/mcp/x/${ID}/settings`, ID),
    ).toBeUndefined();
  });

  it("matches a url-encoded id segment", () => {
    expect(
      activeTabFromPath(
        `/acme/projects/demo/mcp/gateway/${encodeURIComponent("a b")}/members`,
        "a b",
      ),
    ).toBe("members");
  });

  it("returns undefined without an id", () => {
    expect(activeTabFromPath(`${BASE}/overview`, "")).toBeUndefined();
  });
});

describe("initialTabFromHash", () => {
  it("honours a hash naming a real tab", () => {
    expect(initialTabFromHash("#settings")).toBe("settings");
  });

  it("falls back to overview for empty or unknown hashes", () => {
    expect(initialTabFromHash("")).toBe("overview");
    expect(initialTabFromHash("#authentication")).toBe("overview");
  });
});
