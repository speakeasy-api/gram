import { describe, expect, test } from "vitest";

import {
  DEFAULT_PROVIDER_ROWS,
  decodeProviderRows,
  encodeProviderRows,
  rowsFromSearch,
  searchFromRows,
  stokenSearchSchema,
  type ProviderRow,
} from "./url-state";

const SHARED_ROWS: ProviderRow[] = [
  {
    id: "row-1",
    provider: "other",
    customName: "Mistral 日本語",
    providerTokens: "11M",
    tokenizerMin: "1.10",
    tokenizerMax: "1.10",
  },
  {
    id: "row-2",
    provider: "anthropic",
    customName: "",
    providerTokens: "120M",
    tokenizerMin: "1.20",
    tokenizerMax: "1.60",
  },
];

function encodeRawJson(value: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary)
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

describe("provider row URL state", () => {
  test("round-trips all row fields through UTF-8 Base64URL", () => {
    const encoded = encodeProviderRows(SHARED_ROWS);

    expect(encoded).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(encoded).not.toContain("=");
    expect(decodeProviderRows(encoded)).toEqual(SHARED_ROWS);
  });

  test.each(["", "a", "%5B%5D", "not+base64", "💥"])(
    "rejects malformed encoded value %p",
    (value) => {
      expect(decodeProviderRows(value)).toBeNull();
    },
  );

  test("rejects decoded JSON outside the provider-row contract", () => {
    expect(
      decodeProviderRows(encodeRawJson({ provider: "openai" })),
    ).toBeNull();
    expect(
      decodeProviderRows(
        encodeRawJson([
          { ...SHARED_ROWS[0], id: "row-1" },
          { ...SHARED_ROWS[1], id: "row-1" },
        ]),
      ),
    ).toBeNull();
  });

  test("preserves an explicitly empty worksheet", () => {
    expect(decodeProviderRows(encodeProviderRows([]))).toEqual([]);
  });
});

describe("stoken search schema", () => {
  test("keeps a rows value that decodes and drops one that does not", () => {
    const encoded = encodeProviderRows(SHARED_ROWS);

    expect(stokenSearchSchema({ rows: encoded })).toEqual({ rows: encoded });
    expect(stokenSearchSchema({ rows: "not+base64" })).toEqual({});
    expect(stokenSearchSchema({ rows: 123 })).toEqual({});
    expect(stokenSearchSchema({})).toEqual({});
  });

  test("reads the default worksheet from an absent param", () => {
    expect(rowsFromSearch({})).toEqual(DEFAULT_PROVIDER_ROWS);
    expect(rowsFromSearch({ rows: encodeProviderRows(SHARED_ROWS) })).toEqual(
      SHARED_ROWS,
    );
  });

  test("writes the default worksheet as no param at all", () => {
    expect(searchFromRows(DEFAULT_PROVIDER_ROWS)).toEqual({});
    expect(searchFromRows([...DEFAULT_PROVIDER_ROWS])).toEqual({});
  });

  test("writes any other worksheet, an empty one included", () => {
    expect(searchFromRows(SHARED_ROWS)).toEqual({
      rows: encodeProviderRows(SHARED_ROWS),
    });
    expect(searchFromRows([])).toEqual({ rows: encodeProviderRows([]) });
  });

  test("accepts a link written by the standalone estimator", () => {
    // Captured from speakeasy-api/stoken-estimator: one Anthropic row at 120M.
    const standalone =
      "W3siaWQiOiJyb3ctMSIsInByb3ZpZGVyIjoiYW50aHJvcGljIiwiY3VzdG9tTmFtZSI6IiIsInByb3ZpZGVyVG9rZW5zIjoiMTIwTSIsInRva2VuaXplck1pbiI6IjEuMjAiLCJ0b2tlbml6ZXJNYXgiOiIxLjYwIn1d";

    expect(rowsFromSearch(stokenSearchSchema({ rows: standalone }))).toEqual([
      {
        id: "row-1",
        provider: "anthropic",
        customName: "",
        providerTokens: "120M",
        tokenizerMin: "1.20",
        tokenizerMax: "1.60",
      },
    ]);
  });
});
