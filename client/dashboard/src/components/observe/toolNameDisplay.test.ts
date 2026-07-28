import { describe, expect, it } from "vitest";
import { formatToolName } from "./toolNameDisplay";

describe("formatToolName", () => {
  it("returns the last segment of a namespaced Cowork tool name", () => {
    expect(formatToolName("mcp__abc-123__send_message")).toBe("send_message");
  });

  it("uses only the segment after the final separator", () => {
    expect(formatToolName("mcp__server__group__do_thing")).toBe("do_thing");
  });

  it("leaves plain tool names unchanged", () => {
    expect(formatToolName("send_message")).toBe("send_message");
  });

  it("preserves single underscores within a tool name", () => {
    expect(formatToolName("get_user_profile")).toBe("get_user_profile");
  });

  it("falls back to the original when the separator is trailing", () => {
    expect(formatToolName("mcp__abc__")).toBe("mcp__abc__");
  });

  it("handles an empty string", () => {
    expect(formatToolName("")).toBe("");
  });
});
