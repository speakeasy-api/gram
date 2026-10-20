import type { RiskResult } from "@gram/client/models/components/riskresult.js";

/** "a" or "an" for the word that follows, by its first letter. */
function indefiniteArticle(word: string): "a" | "an" {
  const w = word.trim();
  // Initialisms are read letter by letter: "an MCP", "a URL".
  if (/^[A-Z]{2,}\b/.test(w)) return /^[AEFHILMNORSX]/.test(w) ? "an" : "a";
  // A "u" read as "you" ("user", "unique") takes "a".
  if (/^u[^aeiou][aeiou]/i.test(w)) return "a";
  return /^[aeiou]/i.test(w) ? "an" : "a";
}

/** `word` prefixed with "a" or "an". */
export function withArticle(word: string): string {
  return `${indefiniteArticle(word)} ${word}`;
}

/** HH:MM:SS, with .mmm only when the timestamp has sub-second precision. */
export function formatCallTime(date: Date): string {
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  const ms = date.getMilliseconds();
  return ms === 0 ? time : `${time}.${pad(ms, 3)}`;
}

/** Where a finding matched: its span's field and JSON sub-path, if reported. */
export function spanLocation(result: RiskResult): string | undefined {
  const span = result.spans?.find((s) => s.path || s.field);
  if (!span) return undefined;
  return [span.field, span.path].filter(Boolean).join(".");
}

/**
 * "{location} · bytes {start}–{end}" for a sibling row. Offsets are the
 * server's UTF-8 byte offsets, shown as-is so they don't shift on reveal.
 */
export function siblingLocationLabel(result: RiskResult): string {
  const { startPos: start, endPos: end } = result;
  const range = start != null && end != null ? `bytes ${start}–${end}` : null;
  return [spanLocation(result), range].filter(Boolean).join(" · ");
}
