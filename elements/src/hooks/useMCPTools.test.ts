import { asMcpUrls } from "@/lib/api";
import { describe, expect, it, vi } from "vitest";
import { mergeMcpTools } from "./useMCPTools";

describe("asMcpUrls", () => {
  it("returns [] for undefined", () => {
    expect(asMcpUrls(undefined)).toEqual([]);
  });

  it("returns [] for empty string", () => {
    expect(asMcpUrls("")).toEqual([]);
  });

  it("wraps a single URL into an array", () => {
    expect(asMcpUrls("https://example.com/mcp/a")).toEqual([
      "https://example.com/mcp/a",
    ]);
  });

  it("passes through an array of URLs and preserves ordering", () => {
    const urls = [
      "https://example.com/mcp/a",
      "https://example.com/mcp/ai-insights",
    ];
    expect(asMcpUrls(urls)).toEqual(urls);
  });

  it("drops empty-string entries from an array", () => {
    expect(asMcpUrls(["", "https://example.com/mcp/a", ""])).toEqual([
      "https://example.com/mcp/a",
    ]);
  });
});

// mergeMcpTools stands in for "does the array form work?". We use plain
// object stubs rather than mocking @ai-sdk/mcp because the real MCP client
// returns `Record<string, unknown>` where each tool value is opaque to us
// — the merge behavior is the contract, not the transport.
//
// Importantly: each tool value carries its own execute() closure bound to
// the originating MCP client. Merging preserves reference identity, so tool
// calls route back to the correct server.
describe("mergeMcpTools", () => {
  const urlA = "https://example.com/mcp/observability";
  const urlB = "https://example.com/mcp/ai-insights";

  it("merges tools from two MCPs into one record", () => {
    const executeA = vi.fn();
    const executeB = vi.fn();
    const merged = mergeMcpTools([
      { url: urlA, tools: { tool_a: { execute: executeA } } as never },
      { url: urlB, tools: { tool_b: { execute: executeB } } as never },
    ]);
    expect(Object.keys(merged).sort()).toEqual(["tool_a", "tool_b"]);
  });

  it("preserves tool-call routing by preserving reference identity", () => {
    // Each stub carries its own execute closure. After merge, calling a
    // tool's execute() should invoke the function supplied by the MCP that
    // originally exposed it — i.e. routing survives the merge.
    const executeA = vi.fn(() => Promise.resolve("from-a"));
    const executeB = vi.fn(() => Promise.resolve("from-b"));
    const merged = mergeMcpTools([
      {
        url: urlA,
        tools: { gram_search_logs: { execute: executeA } } as never,
      },
      {
        url: urlB,
        tools: { insights_propose_variation: { execute: executeB } } as never,
      },
    ]);

    const toolA = (
      merged as unknown as Record<string, { execute: () => unknown }>
    ).gram_search_logs;
    const toolB = (
      merged as unknown as Record<string, { execute: () => unknown }>
    ).insights_propose_variation;

    toolA?.execute();
    toolB?.execute();
    expect(executeA).toHaveBeenCalledTimes(1);
    expect(executeB).toHaveBeenCalledTimes(1);
  });

  it("warns and last-source-wins on name collision", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const executeFirst = vi.fn();
    const executeSecond = vi.fn();
    const merged = mergeMcpTools([
      { url: urlA, tools: { same_name: { execute: executeFirst } } as never },
      { url: urlB, tools: { same_name: { execute: executeSecond } } as never },
    ]);

    const tool = (
      merged as unknown as Record<string, { execute: () => unknown }>
    ).same_name;
    tool?.execute();
    expect(executeFirst).not.toHaveBeenCalled();
    expect(executeSecond).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn.mock.calls[0]?.[0]).toContain("same_name");
    expect(warn.mock.calls[0]?.[0]).toContain(urlA);
    expect(warn.mock.calls[0]?.[0]).toContain(urlB);
    warn.mockRestore();
  });

  it("returns an empty record for no sources", () => {
    expect(mergeMcpTools([])).toEqual({});
  });
});
