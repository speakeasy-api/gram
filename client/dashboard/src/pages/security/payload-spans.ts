import type { RiskResult } from "@gram/client/models/components/riskresult.js";

/** A finding's span as UTF-8 byte offsets into the scanned payload. */
export type ByteSpan = { id: string; startByte: number; endByte: number };

/** Merged `[start, end)` JS string-index range and the findings covering it. */
export type SpanRange = { start: number; end: number; ids: string[] };

function utf8Length(codePoint: number): number {
  if (codePoint < 0x80) return 1;
  if (codePoint < 0x800) return 2;
  if (codePoint < 0x10000) return 3;
  return 4;
}

/**
 * Maps UTF-8 byte offsets to UTF-16 string indices in one pass over `text`.
 * An offset inside a multibyte character rounds up to the next boundary;
 * offsets past the end of the text are left out of the result.
 */
export function byteOffsetsToIndices(
  text: string,
  offsets: readonly number[],
): Map<number, number> {
  const wanted = [...new Set(offsets)]
    .filter((o) => Number.isInteger(o) && o >= 0)
    .sort((a, b) => a - b);
  const out = new Map<number, number>();
  let w = 0;
  let byte = 0;
  let i = 0;
  for (;;) {
    while (w < wanted.length && wanted[w]! <= byte) {
      out.set(wanted[w]!, i);
      w++;
    }
    if (w >= wanted.length || i >= text.length) break;
    const cp = text.codePointAt(i)!;
    byte += utf8Length(cp);
    i += cp > 0xffff ? 2 : 1;
  }
  return out;
}

/** Every byte span a finding reports: its `spans[]`, else its start/end. */
export function findingByteSpans(result: RiskResult): ByteSpan[] {
  const spans: ByteSpan[] = [];
  for (const span of result.spans ?? []) {
    if (span.startPos == null || span.endPos == null) continue;
    spans.push({
      id: result.id,
      startByte: span.startPos,
      endByte: span.endPos,
    });
  }
  if (spans.length > 0) return spans;
  if (result.startPos == null || result.endPos == null) return [];
  return [
    { id: result.id, startByte: result.startPos, endByte: result.endPos },
  ];
}

/** Mirrors MaxMCPExecutionPayloadBytes: the server stores at most this much. */
export const STORED_PAYLOAD_MAX_BYTES = 64 * 1024;

// The server cuts at a rune boundary, so a capped payload can be up to three
// bytes short of the cap.
const MAX_RUNE_BACKOFF_BYTES = 3;

/**
 * Converts byte spans to merged string-index ranges over `payload`. Spans that
 * fall outside the payload are dropped and reported through `complete`: some
 * sources index a different string than the one stored, so a masked view must
 * not trust the payload once a span is lost. A lost span that ends past a
 * payload stored at the size cap is listed in `truncatedIds`. Empty spans are
 * dropped silently.
 */
export function buildSpanRanges(
  payload: string,
  spans: readonly ByteSpan[],
): { ranges: SpanRange[]; complete: boolean; truncatedIds: string[] } {
  const index = byteOffsetsToIndices(
    payload,
    spans.flatMap((s) => [s.startByte, s.endByte]),
  );
  let complete = true;
  let payloadBytes: number | undefined;
  const truncatedIds: string[] = [];
  const mapped: SpanRange[] = [];
  for (const span of spans) {
    if (span.endByte === span.startByte) continue;
    const start = index.get(span.startByte);
    const end = index.get(span.endByte);
    if (start === undefined || end === undefined || end <= start) {
      complete = false;
      payloadBytes ??= new TextEncoder().encode(payload).length;
      const capped =
        payloadBytes >= STORED_PAYLOAD_MAX_BYTES - MAX_RUNE_BACKOFF_BYTES;
      if (capped && span.endByte > payloadBytes) truncatedIds.push(span.id);
      continue;
    }
    mapped.push({ start, end, ids: [span.id] });
  }
  mapped.sort((a, b) => a.start - b.start || a.end - b.end);

  const merged: SpanRange[] = [];
  for (const range of mapped) {
    const last = merged[merged.length - 1];
    if (last && range.start < last.end) {
      last.end = Math.max(last.end, range.end);
      for (const id of range.ids) {
        if (!last.ids.includes(id)) last.ids.push(id);
      }
    } else {
      merged.push({ ...range, ids: [...range.ids] });
    }
  }
  return { ranges: merged, complete, truncatedIds };
}

export type PayloadTokenKind = "key" | "str" | "num" | "punc" | "text";

export type PayloadSegment = {
  text: string;
  kind: PayloadTokenKind;
  /** Index into the ranges passed to `layoutPayload`. */
  range?: number;
  /** This segment opens its range (where a masked chip renders). */
  rangeStart?: boolean;
};

export type PayloadLine = PayloadSegment[];

// A slice of the raw payload, or layout whitespace that is not in it.
type Piece =
  | { start: number; end: number; kind: PayloadTokenKind }
  | { text: string };

const JSON_TOKEN =
  /"(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null|[{}[\],:]|\s+/y;

type Token = { start: number; end: number; kind: PayloadTokenKind };

function tokenizeJson(payload: string): Token[] | null {
  const tokens: Token[] = [];
  JSON_TOKEN.lastIndex = 0;
  while (JSON_TOKEN.lastIndex < payload.length) {
    const start = JSON_TOKEN.lastIndex;
    const m = JSON_TOKEN.exec(payload);
    if (!m) return null;
    const text = m[0];
    if (/^\s/.test(text)) continue;
    let kind: PayloadTokenKind = "num";
    if (text.startsWith('"')) kind = "str";
    else if (/^[{}[\],:]$/.test(text)) kind = "punc";
    tokens.push({ start, end: start + text.length, kind });
  }
  for (let k = 0; k < tokens.length - 1; k++) {
    const next = tokens[k + 1]!;
    if (tokens[k]!.kind === "str" && payload[next.start] === ":") {
      tokens[k]!.kind = "key";
    }
  }
  return tokens;
}

// Pretty-prints JSON tokens two-space indented. Only whitespace is synthetic,
// so every token still points at its raw byte range.
function jsonLines(payload: string, tokens: Token[]): Piece[][] {
  const lines: Piece[][] = [];
  let indent = 0;
  let line: Piece[] = [];
  const newline = () => {
    lines.push(line);
    line = indent > 0 ? [{ text: "  ".repeat(indent) }] : [];
  };
  for (let k = 0; k < tokens.length; k++) {
    const tok = tokens[k]!;
    const c = tok.kind === "punc" ? payload[tok.start] : "";
    if (c === "{" || c === "[") {
      line.push(tok);
      const next = tokens[k + 1];
      const close = c === "{" ? "}" : "]";
      if (next && payload[next.start] === close) {
        line.push(next);
        k++;
        continue;
      }
      indent++;
      newline();
    } else if (c === "}" || c === "]") {
      indent = Math.max(0, indent - 1);
      newline();
      line.push(tok);
    } else if (c === ",") {
      line.push(tok);
      newline();
    } else if (c === ":") {
      line.push(tok, { text: " " });
    } else {
      line.push(tok);
    }
  }
  lines.push(line);
  return lines;
}

function textLines(payload: string): Piece[][] {
  const lines: Piece[][] = [];
  let start = 0;
  for (;;) {
    const nl = payload.indexOf("\n", start);
    const end = nl === -1 ? payload.length : nl;
    lines.push(end > start ? [{ start, end, kind: "text" }] : []);
    if (nl === -1) break;
    start = nl + 1;
  }
  return lines;
}

/** Whether the payload is JSON and renders with syntax colors. */
function isJsonPayload(payload: string): boolean {
  try {
    JSON.parse(payload);
    return true;
  } catch {
    return false;
  }
}

/**
 * Lays the raw payload out as display lines, split at range boundaries. JSON
 * is reflowed and tokenized for syntax color; anything else renders as plain
 * text lines. Ranges index the raw payload, so reflowing never moves them.
 */
export function layoutPayload(
  payload: string,
  ranges: readonly SpanRange[],
): PayloadLine[] {
  const tokens = isJsonPayload(payload) ? tokenizeJson(payload) : null;
  const lines = tokens ? jsonLines(payload, tokens) : textLines(payload);
  // A range opening in reflowed whitespace opens at its first emitted segment.
  const opened = new Set<number>();
  return lines.map((pieces) => {
    const out: PayloadSegment[] = [];
    for (const piece of pieces) {
      if ("text" in piece) {
        out.push({ text: piece.text, kind: "text" });
        continue;
      }
      let pos = piece.start;
      ranges.forEach((r, ri) => {
        if (r.end <= piece.start || r.start >= piece.end) return;
        if (r.start > pos) {
          out.push({ text: payload.slice(pos, r.start), kind: piece.kind });
        }
        const s = Math.max(r.start, pos);
        const e = Math.min(r.end, piece.end);
        out.push({
          text: payload.slice(s, e),
          kind: piece.kind,
          range: ri,
          rangeStart: !opened.has(ri),
        });
        opened.add(ri);
        pos = e;
      });
      if (pos < piece.end) {
        out.push({ text: payload.slice(pos, piece.end), kind: piece.kind });
      }
    }
    return out;
  });
}

/** Chip label for a masked span: "redacted · {length}". */
export function redactionChipLabel(length: number): string {
  return `redacted · ${length}`;
}
