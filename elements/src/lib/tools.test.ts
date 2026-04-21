import { describe, expect, it, vi } from "vitest";
import {
  capToolResultBytes,
  truncateTextToByteCap,
  wrapToolsWithByteCap,
} from "./tools";
import type { ToolSet } from "ai";

describe("truncateTextToByteCap", () => {
  it("returns original when under cap", () => {
    expect(truncateTextToByteCap("hello world", 100)).toBe("hello world");
  });

  it("truncates with head + tail + notice when over cap", () => {
    const text = "a".repeat(1000) + "-MIDDLE-" + "b".repeat(1000);
    const out = truncateTextToByteCap(text, 200);
    expect(out.length).toBeLessThan(text.length);
    expect(out).toContain("tool output truncated");
    expect(out.startsWith("a")).toBe(true);
    expect(out.endsWith("b")).toBe(true);
    expect(out).not.toContain("MIDDLE");
  });

  it("passes through when maxBytes <= 0 (disabled)", () => {
    const text = "x".repeat(10_000);
    expect(truncateTextToByteCap(text, 0)).toBe(text);
    expect(truncateTextToByteCap(text, -1)).toBe(text);
  });

  it("handles multibyte UTF-8 without crashing", () => {
    const text = "🎉".repeat(500);
    const out = truncateTextToByteCap(text, 200);
    expect(out).toContain("tool output truncated");
    expect(new TextEncoder().encode(out).byteLength).toBeGreaterThan(0);
  });
});

describe("capToolResultBytes", () => {
  it("truncates plain string results", () => {
    const out = capToolResultBytes("x".repeat(5_000), 100);
    expect(typeof out).toBe("string");
    expect(out).not.toBe("x".repeat(5_000));
    expect(out).toContain("tool output truncated");
  });

  it("truncates text chunks inside MCP-shaped results", () => {
    const result = {
      content: [
        { type: "text", text: "short" },
        { type: "text", text: "big".repeat(5_000) },
      ],
      isError: false,
    };
    const out = capToolResultBytes(result, 100) as typeof result;
    expect(out.content[0]).toEqual({ type: "text", text: "short" });
    expect((out.content[1] as { text: string }).text).toContain(
      "tool output truncated",
    );
    expect(out.isError).toBe(false);
  });

  it("leaves non-text chunks alone", () => {
    const result = {
      content: [
        { type: "image", data: "x".repeat(5_000), mimeType: "image/png" },
      ],
    };
    const out = capToolResultBytes(result, 100) as typeof result;
    expect(out.content[0]).toEqual(result.content[0]);
  });

  it("preserves isError flag", () => {
    const result = {
      content: [{ type: "text", text: "tool blew up: " + "x".repeat(5_000) }],
      isError: true,
    };
    const out = capToolResultBytes(result, 100) as typeof result;
    expect(out.isError).toBe(true);
  });

  it("passes unknown shapes through", () => {
    expect(capToolResultBytes(42, 100)).toBe(42);
    expect(capToolResultBytes(null, 100)).toBe(null);
    expect(capToolResultBytes({ foo: "bar" }, 100)).toEqual({ foo: "bar" });
  });
});

describe("wrapToolsWithByteCap", () => {
  it("is a no-op when maxBytes is undefined/0", () => {
    const execute = vi.fn().mockResolvedValue("anything");
    const tools: ToolSet = {
      t: { description: "", inputSchema: { type: "object" }, execute } as never,
    };
    expect(wrapToolsWithByteCap(tools, undefined)).toBe(tools);
    expect(wrapToolsWithByteCap(tools, 0)).toBe(tools);
  });

  it("wraps execute and truncates oversized result", async () => {
    const execute = vi.fn().mockResolvedValue({
      content: [{ type: "text", text: "z".repeat(10_000) }],
    });
    const tools: ToolSet = {
      t: { description: "", inputSchema: { type: "object" }, execute } as never,
    };
    const wrapped = wrapToolsWithByteCap(tools, 256);
    const wrappedExecute = wrapped.t.execute!;
    const out = (await wrappedExecute({}, { toolCallId: "id" } as never)) as {
      content: Array<{ text: string }>;
    };
    expect(out.content[0]!.text).toContain("tool output truncated");
    expect(execute).toHaveBeenCalledOnce();
  });

  it("leaves tools without execute alone", () => {
    const tools: ToolSet = {
      t: { description: "", inputSchema: { type: "object" } } as never,
    };
    const wrapped = wrapToolsWithByteCap(tools, 256);
    expect(wrapped.t).toBe(tools.t);
  });
});
