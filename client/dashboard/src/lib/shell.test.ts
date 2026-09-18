import { describe, expect, it } from "vitest";
import { shellQuote } from "./shell";

describe("shellQuote", () => {
  it("wraps a plain value in single quotes", () => {
    expect(shellQuote("Petstore API")).toBe("'Petstore API'");
  });

  it("neutralizes expansions and metacharacters", () => {
    expect(shellQuote('$(rm -rf /) `x` "y" ; z')).toBe(
      `'$(rm -rf /) \`x\` "y" ; z'`,
    );
  });

  it("escapes embedded single quotes", () => {
    expect(shellQuote("Bob's API")).toBe(`'Bob'\\''s API'`);
  });

  it("quotes the empty string", () => {
    expect(shellQuote("")).toBe("''");
  });
});
