// The worksheet's URL state: provider rows encoded as unpadded UTF-8 Base64URL
// in the `rows` search param. Ported from speakeasy-api/stoken-estimator with
// the same wire format, so a link copied from the standalone estimator pastes
// onto the admin route and restores the same worksheet.
//
// The nuqs parser the standalone app used is replaced by a search schema the
// route's `validateSearch` runs, which is how every other admin list keeps its
// state in the URL.

export type ProviderKey = "openai" | "anthropic" | "gemini" | "other";

export type ProviderRow = {
  id: string;
  provider: ProviderKey;
  customName: string;
  providerTokens: string;
  tokenizerMin: string;
  tokenizerMax: string;
};

export const DEFAULT_PROVIDER_ROWS: ProviderRow[] = [
  {
    id: "row-1",
    provider: "openai",
    customName: "",
    providerTokens: "",
    tokenizerMin: "1.00",
    tokenizerMax: "1.00",
  },
];

const PROVIDER_KEYS: Record<ProviderKey, true> = {
  openai: true,
  anthropic: true,
  gemini: true,
  other: true,
};
const ROW_ID_PATTERN = /^row-\d+$/;
const BASE64URL_PATTERN = /^[A-Za-z0-9_-]+$/;
const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder("utf-8", { fatal: true });

function validateProviderRows(value: unknown): ProviderRow[] | null {
  if (!Array.isArray(value)) return null;

  const rows: ProviderRow[] = [];
  const ids = new Set<string>();
  for (const candidate of value) {
    if (!candidate || typeof candidate !== "object") return null;

    const row = candidate as Record<string, unknown>;
    if (
      typeof row["id"] !== "string" ||
      row["id"].length > 32 ||
      !ROW_ID_PATTERN.test(row["id"]) ||
      ids.has(row["id"]) ||
      typeof row["provider"] !== "string" ||
      // Own keys only: `in` walks the prototype, so a crafted link naming
      // "toString" or "constructor" would otherwise pass as a provider.
      !Object.hasOwn(PROVIDER_KEYS, row["provider"]) ||
      typeof row["customName"] !== "string" ||
      typeof row["providerTokens"] !== "string" ||
      typeof row["tokenizerMin"] !== "string" ||
      typeof row["tokenizerMax"] !== "string"
    ) {
      return null;
    }

    ids.add(row["id"]);
    rows.push({
      id: row["id"],
      provider: row["provider"] as ProviderKey,
      customName: row["customName"],
      providerTokens: row["providerTokens"],
      tokenizerMin: row["tokenizerMin"],
      tokenizerMax: row["tokenizerMax"],
    });
  }

  return rows;
}

function providerRowsEqual(left: ProviderRow[], right: ProviderRow[]): boolean {
  if (left.length !== right.length) return false;

  return left.every((row, index) => {
    const other = right[index];
    return (
      other !== undefined &&
      row.id === other.id &&
      row.provider === other.provider &&
      row.customName === other.customName &&
      row.providerTokens === other.providerTokens &&
      row.tokenizerMin === other.tokenizerMin &&
      row.tokenizerMax === other.tokenizerMax
    );
  });
}

export function encodeProviderRows(rows: ProviderRow[]): string {
  const bytes = textEncoder.encode(JSON.stringify(rows));
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 32_768) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 32_768));
  }

  return btoa(binary)
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

export function decodeProviderRows(value: string): ProviderRow[] | null {
  if (!BASE64URL_PATTERN.test(value) || value.length % 4 === 1) return null;

  try {
    const base64 = value.replaceAll("-", "+").replaceAll("_", "/");
    const paddedBase64 = base64.padEnd(
      base64.length + ((4 - (base64.length % 4)) % 4),
      "=",
    );
    const binary = atob(paddedBase64);
    const bytes = Uint8Array.from(binary, (character) =>
      character.charCodeAt(0),
    );
    return validateProviderRows(JSON.parse(textDecoder.decode(bytes)));
  } catch {
    return null;
  }
}

/** The one search param the calculator keeps. Absent means the default row. */
export type StokenSearch = {
  rows?: string;
};

/**
 * Reads the `rows` param the way the standalone estimator's parser did: a
 * value that does not decode to valid rows is treated as absent, so a mangled
 * link opens the default worksheet rather than a broken one. The param is kept
 * encoded here and decoded by the page, so the URL never carries a value a
 * reload would refuse.
 */
export function stokenSearchSchema(
  search: Record<string, unknown>,
): StokenSearch {
  const value = search["rows"];
  if (typeof value !== "string" || decodeProviderRows(value) === null) {
    return {};
  }
  return { rows: value };
}

/** The rows a validated search holds; the default row when it holds none. */
export function rowsFromSearch(search: StokenSearch): ProviderRow[] {
  const rows =
    search.rows === undefined ? null : decodeProviderRows(search.rows);
  return rows ?? DEFAULT_PROVIDER_ROWS;
}

/**
 * What the URL should say for a set of rows. The default worksheet is written
 * as no param at all, so a link only carries what the operator changed. An
 * explicitly empty worksheet is not the default, and is kept.
 */
export function searchFromRows(rows: ProviderRow[]): StokenSearch {
  if (providerRowsEqual(rows, DEFAULT_PROVIDER_ROWS)) return {};
  return { rows: encodeProviderRows(rows) };
}
