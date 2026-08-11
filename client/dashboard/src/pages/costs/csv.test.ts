import { describe, expect, it } from "vitest";
import { toCsv } from "./csv";

// Exercised through toCsv rather than by exporting csvField: the serializer is
// the surface every export actually calls, and an export used only by a test is
// dead code to knip.

// One row, one cell — the field-level behaviour without the header noise.
const cell = (value: string | number) =>
  toCsv(["h"], [[value]]).split("\r\n")[1];

describe("formula injection (CWE-1236)", () => {
  // Directory-sync values (names, emails) reach these cells, so a display name
  // is an injection vector into whoever opens the export.
  type Case = { name: string; input: string; expected: string };
  const NEUTRALIZED: Case[] = [
    {
      name: "an equals formula",
      input: "=SUM(A1:A10)",
      expected: "'=SUM(A1:A10)",
    },
    { name: "a plus formula", input: "+1+1", expected: "'+1+1" },
    { name: "a minus formula", input: "-1+1", expected: "'-1+1" },
    { name: "an at formula", input: "@SUM(A1)", expected: "'@SUM(A1)" },
    // Importers skip leading whitespace before deciding it's a formula.
    {
      name: "a space-padded formula",
      input: " =SUM(A1:A10)",
      expected: "' =SUM(A1:A10)",
    },
    {
      name: "a multi-space-padded formula",
      input: "   @SUM(A1)",
      expected: "'   @SUM(A1)",
    },
  ];

  it.each(NEUTRALIZED)("neutralizes $name", ({ input, expected }) => {
    expect(cell(input)).toBe(expected);
  });

  // These carry a CR/LF/tab, so they're also quoted — assert the whole cell.
  type QuotedCase = { name: string; input: string; expected: string };
  const QUOTED: QuotedCase[] = [
    {
      name: "a newline-led formula",
      input: "\n=CMD|' /C calc'!A0",
      expected: `"'\n=CMD|' /C calc'!A0"`,
    },
    {
      name: "a carriage-return-led formula",
      input: "\r=SUM(A1)",
      expected: `"'\r=SUM(A1)"`,
    },
  ];

  it.each(QUOTED)("neutralizes and quotes $name", ({ input, expected }) => {
    expect(cell(input)).toBe(expected);
  });

  // A tab is a formula trigger but not a CSV delimiter, so it's neutralized
  // without being quoted.
  it.each([
    { name: "a tab-led formula", input: "\t=SUM(A1)", expected: "'\t=SUM(A1)" },
    // A leading control char is a trigger even with no =/+/-/@ after it.
    { name: "a bare tab lead", input: "\tplain", expected: "'\tplain" },
  ])("neutralizes $name without quoting it", ({ input, expected }) => {
    expect(cell(input)).toBe(expected);
  });

  it.each([
    ["a plain name", "Olivia Novak"],
    ["an email", "adam@speakeasy.com"],
    ["a name with an inner equals", "budget=q3"],
  ])("leaves %s alone", (_name, input) => {
    expect(cell(input)).toBe(input);
  });

  // Guarding numbers would corrupt a negative into the text "'-5".
  it.each([
    [0, "0"],
    [42, "42"],
    [-5, "-5"],
    [-0.46, "-0.46"],
  ])("passes the number %s through as %s", (input, expected) => {
    expect(cell(input)).toBe(expected);
  });
});

describe("RFC 4180 quoting", () => {
  it.each([
    {
      name: "a comma",
      input: "R&D, Engineering",
      expected: `"R&D, Engineering"`,
    },
    { name: "a quote", input: 'say "hi"', expected: `"say ""hi"""` },
    { name: "a newline", input: "line1\nline2", expected: `"line1\nline2"` },
    {
      name: "a carriage return",
      input: "line1\rline2",
      expected: `"line1\rline2"`,
    },
  ])("quotes $name", ({ input, expected }) => {
    expect(cell(input)).toBe(expected);
  });
});

describe("toCsv", () => {
  it("separates records with CRLF", () => {
    expect(
      toCsv(
        ["a", "b"],
        [
          ["1", "2"],
          ["3", "4"],
        ],
      ),
    ).toBe("a,b\r\n1,2\r\n3,4");
  });

  it("emits a header-only file when there are no rows", () => {
    expect(toCsv(["a", "b"], [])).toBe("a,b");
  });

  it("does not terminate the last record", () => {
    expect(toCsv(["a"], [["1"]]).endsWith("1")).toBe(true);
  });
});
