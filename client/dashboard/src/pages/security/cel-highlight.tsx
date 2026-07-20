import type { ReactNode } from "react";
import type { CelSpan } from "./cel-wasm";

// Wraps each matched range in a <mark>. The engine's offsets are UTF-8 bytes, so
// we slice in byte space and decode (correct for non-ASCII).
export function highlight(text: string, spans: CelSpan[]): ReactNode[] {
  const ranges = spans
    .map((s) => ({ start: s.Start, end: s.End }))
    .filter((r) => r.end > r.start)
    .sort((a, b) => a.start - b.start);
  if (ranges.length === 0) return [text];

  const bytes = new TextEncoder().encode(text);
  const dec = new TextDecoder();
  const nodes: ReactNode[] = [];
  let cursor = 0;
  let key = 0;
  for (const r of ranges) {
    if (r.end <= cursor) continue; // already covered by an overlapping range
    const start = Math.max(r.start, cursor);
    if (start > cursor) nodes.push(dec.decode(bytes.slice(cursor, start)));
    nodes.push(
      <mark
        key={key++}
        className="bg-warning/30 text-foreground rounded-sm px-0.5"
      >
        {dec.decode(bytes.slice(start, r.end))}
      </mark>,
    );
    cursor = r.end;
  }
  if (cursor < bytes.length) nodes.push(dec.decode(bytes.slice(cursor)));
  return nodes;
}
