import { describe, expect, it } from "vitest";
import {
  isAnthropicSource,
  isCursorSource,
  isOtherPlatformSource,
} from "./hook-event-sources";

describe("hook event sources", () => {
  it("keeps Cursor off the Anthropic card", () => {
    expect(isAnthropicSource("cursor")).toBe(false);
    expect(isAnthropicSource("claude-code")).toBe(true);
    expect(isAnthropicSource("cowork")).toBe(true);
  });

  it("confirms Cursor on both the Cursor card and the device-agent card", () => {
    expect(isCursorSource("cursor")).toBe(true);
    expect(isOtherPlatformSource("cursor")).toBe(true);
  });

  it("leaves Anthropic and LiteLLM sources off the device-agent card", () => {
    expect(isOtherPlatformSource("claude-code")).toBe(false);
    expect(isOtherPlatformSource("cowork")).toBe(false);
    expect(isOtherPlatformSource("litellm")).toBe(false);
    expect(isOtherPlatformSource("codex")).toBe(true);
    expect(isOtherPlatformSource("")).toBe(false);
  });
});
