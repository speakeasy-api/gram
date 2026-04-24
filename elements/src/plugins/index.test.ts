import { describe, it, expect } from "vitest";
import { recommended } from "./index";
import { chart } from "./chart";
import { generativeUI } from "./generative-ui";

describe("recommended", () => {
  it("contains chart and generative-ui plugins", () => {
    expect(recommended).toHaveLength(2);
    expect(recommended).toContain(chart);
    expect(recommended).toContain(generativeUI);
  });

  it("is usable as a plain array", () => {
    const ids = recommended.map((p) => p.id);
    expect(ids).toEqual(["chart", "generative-ui"]);
  });
});

describe("recommended.except", () => {
  it("excludes a plugin by id", () => {
    const result = recommended.except("generative-ui");
    expect(result).toHaveLength(1);
    expect(result[0]).toBe(chart);
  });

  it("excludes multiple plugins", () => {
    const result = recommended.except("chart", "generative-ui");
    expect(result).toHaveLength(0);
  });

  it("returns all plugins when no ids match", () => {
    const result = recommended.except("nonexistent");
    expect(result).toHaveLength(2);
  });

  it("returns all plugins when called with no arguments", () => {
    const result = recommended.except();
    expect(result).toHaveLength(2);
  });

  it("does not match on language when id is set", () => {
    // generative-ui has id="generative-ui" and language="ui"
    // Excluding by language value "ui" should not match because id takes precedence
    const result = recommended.except("ui");
    expect(result).toHaveLength(2);
  });
});
