import { describe, it, expect } from "vitest";
import { parseSegments } from "./MessageContent.parser";

describe("parseSegments", () => {
  it("returns empty array for empty content", () => {
    expect(parseSegments("")).toEqual([]);
  });

  it("returns a single text segment when there are no fences", () => {
    expect(parseSegments("Just plain prose, no fences here.")).toEqual([
      { type: "text", text: "Just plain prose, no fences here." },
    ]);
  });

  it("extracts a single chart fence as a block", () => {
    const content = '```chart\n{"type":"BarChart","props":{}}\n```';
    expect(parseSegments(content)).toEqual([
      {
        type: "block",
        lang: "chart",
        code: '{"type":"BarChart","props":{}}\n',
      },
    ]);
  });

  it("extracts a single ui fence as a block", () => {
    const content = '```ui\n{"type":"Card","props":{}}\n```';
    expect(parseSegments(content)).toEqual([
      {
        type: "block",
        lang: "ui",
        code: '{"type":"Card","props":{}}\n',
      },
    ]);
  });

  it("normalises the language to lowercase", () => {
    const content = '```Chart\n{"type":"BarChart"}\n```';
    const segments = parseSegments(content);
    expect(segments[0]).toMatchObject({ type: "block", lang: "chart" });
  });

  it("keeps unsupported language fences as text so they stay visible", () => {
    // ```python is not a widget — render it verbatim, not as an empty block.
    const content = '```python\nprint("hi")\n```';
    expect(parseSegments(content)).toEqual([
      { type: "text", text: '```python\nprint("hi")\n```' },
    ]);
  });

  it("handles text + chart + text + ui + text in order", () => {
    const content = [
      "Here is a chart:",
      "```chart",
      '{"type":"BarChart","props":{"title":"x"}}',
      "```",
      "And a card:",
      "```ui",
      '{"type":"Card","props":{}}',
      "```",
      "Done.",
    ].join("\n");

    const segments = parseSegments(content);
    expect(segments.map((s) => s.type)).toEqual([
      "text",
      "block",
      "text",
      "block",
      "text",
    ]);
    expect(segments[0]).toMatchObject({ type: "text" });
    expect((segments[0] as { text: string }).text).toContain(
      "Here is a chart:",
    );
    expect(segments[1]).toMatchObject({ type: "block", lang: "chart" });
    expect(segments[3]).toMatchObject({ type: "block", lang: "ui" });
    expect((segments[4] as { text: string }).text).toContain("Done.");
  });

  it("handles a chart fence at the very start of content", () => {
    const content = '```chart\n{"type":"BarChart"}\n```\nfollow-up text';
    const segments = parseSegments(content);
    expect(segments[0]).toMatchObject({ type: "block", lang: "chart" });
    expect(segments[1]).toMatchObject({ type: "text" });
  });

  it("handles a chart fence at the very end of content", () => {
    const content = 'leading text\n```chart\n{"type":"BarChart"}\n```';
    const segments = parseSegments(content);
    expect(segments[0]).toMatchObject({ type: "text" });
    expect(segments[1]).toMatchObject({ type: "block", lang: "chart" });
  });

  it("tolerates CRLF line endings between the fence and the body", () => {
    const content = '```chart\r\n{"type":"BarChart"}\r\n```';
    const segments = parseSegments(content);
    expect(segments[0]).toMatchObject({ type: "block", lang: "chart" });
    expect((segments[0] as { code: string }).code).toContain("BarChart");
  });

  it("is repeatable — each call resets the regex state", () => {
    // Guards against the classic /g lastIndex bug where the second call
    // misses content because the regex state leaked from the first run.
    const content = '```chart\n{"type":"BarChart"}\n```';
    const first = parseSegments(content);
    const second = parseSegments(content);
    expect(first).toEqual(second);
  });
});
