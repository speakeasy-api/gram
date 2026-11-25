// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  displayToolName,
  formatArgs,
  isToolError,
  resultText,
} from "./terminal-format";

describe("displayToolName", () => {
  it("strips a double-underscore MCP prefix and the platform_ prefix", () => {
    expect(displayToolName("mcp__platform__platform_list_chats")).toEqual({
      name: "list_chats",
      mcp: true,
    });
  });

  it("strips a single-underscore MCP prefix with a hyphenated server slug", () => {
    expect(
      displayToolName(
        "mcp__p-managed-assistant_platform_get_tool_usage_summary",
      ),
    ).toEqual({ name: "get_tool_usage_summary", mcp: true });
  });

  it("leaves built-in tool names alone", () => {
    expect(displayToolName("tool_search")).toEqual({
      name: "tool_search",
      mcp: false,
    });
  });
});

describe("formatArgs", () => {
  it("renders strings as plain text on one line", () => {
    expect(formatArgs({ script: 'local a = "x"\n  return a' })).toBe(
      'script: "local a = "x" return a"',
    );
  });

  it("renders other values as JSON", () => {
    expect(formatArgs({ limit: 5, tags: ["a"] })).toBe('limit: 5, tags: ["a"]');
  });

  it("returns an empty string for no arguments", () => {
    expect(formatArgs(undefined)).toBe("");
  });
});

describe("resultText", () => {
  it("reads a server tool message", () => {
    expect(resultText({ role: "tool", content: "done" })).toBe("done");
  });

  it("joins MCP text content parts", () => {
    expect(
      resultText({
        content: [
          { type: "text", text: "a" },
          { type: "image" },
          { type: "text", text: "b" },
        ],
      }),
    ).toBe("a\nb");
  });

  it("pretty-prints other objects", () => {
    expect(resultText({ ok: true })).toBe('{\n  "ok": true\n}');
  });
});

describe("isToolError", () => {
  it("is true for incomplete calls", () => {
    expect(isToolError("incomplete", undefined)).toBe(true);
  });

  it("is true when the result flags an error", () => {
    expect(isToolError("complete", { isError: true, content: [] })).toBe(true);
  });

  it("is true for server-side failures reported as text", () => {
    expect(
      isToolError("complete", {
        role: "tool",
        content: '{"err":"tool execution failed: permission denied"}',
      }),
    ).toBe(true);
  });

  it("is false for a successful result that quotes the failure text", () => {
    expect(
      isToolError("complete", {
        role: "tool",
        content: "Last week's chat said: tool execution failed twice.",
      }),
    ).toBe(false);
  });

  it("is false for a normal result", () => {
    expect(
      isToolError("complete", { role: "tool", content: "12 servers" }),
    ).toBe(false);
  });
});
