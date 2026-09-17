import { describe, expect, it } from "vitest";
import { sectionIdForHash } from "./sourceDetailSections";

describe("sectionIdForHash", () => {
  it("accepts current section ids with or without the hash", () => {
    expect(sectionIdForHash("#tools")).toBe("tools");
    expect(sectionIdForHash("settings")).toBe("settings");
  });

  it("maps the old tab hashes onto the sections that replaced them", () => {
    expect(sectionIdForHash("#overview")).toBe("details");
    expect(sectionIdForHash("#mcp-servers")).toBe("details");
    expect(sectionIdForHash("#spec")).toBe("content");
    expect(sectionIdForHash("#deployments")).toBe("versions");
  });

  it("ignores empty and unknown hashes", () => {
    expect(sectionIdForHash("")).toBeNull();
    expect(sectionIdForHash("#")).toBeNull();
    expect(sectionIdForHash("#nope")).toBeNull();
  });
});
