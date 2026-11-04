import { describe, expect, it } from "vitest";
import { servedSkillDigest, shortServedSkillDigest } from "./mcp-served-skills";

describe("servedSkillDigest", () => {
  it("renders the sha256 form agents see over MCP, lowercased", () => {
    expect(servedSkillDigest("ABCDEF0123")).toBe("sha256:abcdef0123");
  });

  it("shortens the digest to a labeled prefix", () => {
    expect(shortServedSkillDigest("0123456789abcdef0123456789abcdef")).toBe(
      "sha256:0123456789ab…",
    );
  });
});
