import { describe, expect, it } from "vitest";
import {
  importCsvHeader,
  importPrompt,
  parseMatrixImport,
  type ImportCatalog,
} from "./importCsv";
import type { Draft } from "./model";

const catalog: ImportCatalog = {
  methods: [{ id: "device", name: "Device Agent" }],
  products: [{ id: "claude-code-cli", name: "Claude Code CLI" }],
  capabilities: [{ id: "session", name: "Session tracking" }],
};
const empty: Draft = { mappings: {}, references: {}, accounts: {} };
const csv = (...rows: string[]) => [importCsvHeader, ...rows].join("\r\n");

describe("matrix CSV import", () => {
  it("preserves multiline notes, escaped quotes, BOM and trailing empty cells", () => {
    const result = parseMatrixImport(
      "\uFEFF" +
        csv(
          'reference,device,,session,partial,"via hooks, says ""verify""\nsecond line",true,,,',
        ),
      catalog,
      empty,
    );
    expect(result.draft.references.device?.session).toEqual({
      status: "partial",
      note: 'via hooks, says "verify"\nsecond line',
      verify: true,
    });
    expect(result.counts).toEqual({
      reference: 1,
      method: 0,
      mapping: 0,
      coverage: 0,
    });
    expect(empty).toEqual({ mappings: {}, references: {}, accounts: {} });
  });

  it("merges supplied entries without losing omitted facts or inventing coverage", () => {
    const current: Draft = {
      accounts: {},
      references: {
        device: {
          untouched: { status: "unknown", note: "keep", verify: true },
        },
      },
      mappings: {
        "device/claude-code-cli": {
          applicability: "applicable",
          conditions: "old",
          accounts: {},
          facts: {
            session: { status: "supported", note: "keep", verify: false },
          },
        },
      },
    };
    const before = structuredClone(current);
    const result = parseMatrixImport(
      csv(
        "mapping,device,claude-code-cli,,,,,applicable,Mac; Linux VERIFY,",
        "reference,device,,session,unknown,,true,,,",
      ),
      catalog,
      current,
    );
    expect(result.draft.references.device?.untouched).toEqual(
      current.references.device?.untouched,
    );
    expect(result.draft.mappings["device/claude-code-cli"]?.facts).toEqual(
      current.mappings["device/claude-code-cli"]?.facts,
    );
    expect(result.draft.mappings["device/claude-code-cli"]?.conditions).toBe(
      "Mac; Linux VERIFY",
    );
    expect(current).toEqual(before);
    const withoutCoverage = parseMatrixImport(
      csv("mapping,device,claude-code-cli,,,,,applicable,✅,"),
      catalog,
      empty,
    );
    expect(
      withoutCoverage.draft.mappings["device/claude-code-cli"]?.facts,
    ).toEqual({});
  });

  it("accepts explicit coverage before its mapping row", () => {
    const result = parseMatrixImport(
      csv(
        "coverage,device,claude-code-cli,session,supported,via hooks,false,,,",
        "mapping,device,claude-code-cli,,,,,applicable,Team only,",
      ),
      catalog,
      empty,
    );
    expect(
      result.draft.mappings["device/claude-code-cli"]?.facts.session?.status,
    ).toBe("supported");
  });

  it.each([
    ["reference,missing,,session,supported,,false,,,", "unknown method_id"],
    ["reference,__proto__,,session,supported,,false,,,", "unknown method_id"],
    ["mapping,device,missing,,,,,applicable,,", "unknown platform_id"],
    ["reference,device,,missing,supported,,false,,,", "unknown capability_id"],
    ["reference,device,,session,yes,,false,,,", "unknown status"],
    ["reference,device,,session,constructor,,false,,,", "unknown status"],
    ["reference,device,,session,partial,,false,,,", "requires a note"],
    ["reference,device,,session,supported,,maybe,,,", "verify must"],
    [
      "reference,device,claude-code-cli,session,supported,,false,,,",
      "leave platform_id empty",
    ],
    [
      "mapping,device,claude-code-cli,session,,,,applicable,,",
      "must leave capability_id",
    ],
    ["mapping,device,claude-code-cli,,,,,supported,,", "applicability must"],
    [
      "coverage,device,claude-code-cli,session,supported,,false,,,",
      "requires an applicable mapping",
    ],
    [
      'reference,device,,session,supported,"unterminated,false,,,',
      "unclosed quoted field",
    ],
    [
      'reference,device,,session,supported,"note"extra,false,,,',
      "Invalid CSV quoting",
    ],
    ["reference,device,,session,supported,,false", "expected 10 columns"],
  ])("rejects invalid input atomically: %s", (row, message) => {
    expect(() => parseMatrixImport(csv(row), catalog, empty)).toThrow(message);
    expect(empty).toEqual({ mappings: {}, references: {}, accounts: {} });
  });

  it("rejects duplicate records, empty files, and oversized files", () => {
    const row = "reference,device,,session,supported,,false,,,";
    expect(() => parseMatrixImport(csv(row, row), catalog, empty)).toThrow(
      "duplicate",
    );
    expect(() => parseMatrixImport(csv(), catalog, empty)).toThrow(
      "no data rows",
    );
    expect(() => parseMatrixImport("wrong,header", catalog, empty)).toThrow(
      "Expected header",
    );
    expect(() =>
      parseMatrixImport("x".repeat(2 * 1024 * 1024 + 1), catalog, empty),
    ).toThrow("2 MB");
  });

  it("counts Unicode code points for note and condition limits", () => {
    const note = "🙂".repeat(10000);
    const conditions = "é".repeat(10000);
    expect(() =>
      parseMatrixImport(
        csv(
          `reference,device,,session,supported,${note},false,,,`,
          `mapping,device,claude-code-cli,,,,,applicable,${conditions},`,
        ),
        catalog,
        empty,
      ),
    ).not.toThrow();
    expect(() =>
      parseMatrixImport(
        csv(`reference,device,,session,supported,${note}🙂,false,,,`),
        catalog,
        empty,
      ),
    ).toThrow("10000 characters");
    expect(() =>
      parseMatrixImport(
        csv(`mapping,device,claude-code-cli,,,,,applicable,${conditions}é,`),
        catalog,
        empty,
      ),
    ).toThrow("10000 characters");
  });

  it("includes the current catalog and header in the agent prompt", () => {
    const prompt = importPrompt(catalog);
    expect(prompt).toContain(importCsvHeader);
    expect(prompt).toContain("device: Device Agent");
    expect(prompt).toContain("claude-code-cli: Claude Code CLI");
    expect(prompt).toContain("session: Session tracking");
  });
});
