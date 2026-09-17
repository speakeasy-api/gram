import { describe, expect, it } from "vitest";
import { tabForHash } from "./sourceDetailSections";

describe("tabForHash", () => {
  it("accepts current tab ids with or without the hash", () => {
    expect(tabForHash("#tools")).toBe("tools");
    expect(tabForHash("settings")).toBe("settings");
  });

  it("maps the old section and tab hashes onto the tabs that hold them", () => {
    expect(tabForHash("#details")).toBe("overview");
    expect(tabForHash("#activity")).toBe("overview");
    expect(tabForHash("#content")).toBe("overview");
    expect(tabForHash("#spec")).toBe("overview");
    expect(tabForHash("#mcp-servers")).toBe("overview");
    expect(tabForHash("#deployments")).toBe("versions");
  });

  it("ignores empty and unknown hashes", () => {
    expect(tabForHash("")).toBeNull();
    expect(tabForHash("#")).toBeNull();
    expect(tabForHash("#nope")).toBeNull();
  });

  it("ignores hashes named after inherited object properties", () => {
    expect(tabForHash("#constructor")).toBeNull();
    expect(tabForHash("#toString")).toBeNull();
    expect(tabForHash("#__proto__")).toBeNull();
  });
});
