import { describe, expect, it } from "vitest";
import type { Chat } from "@gram/client/models/components/chat.js";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { RiskSegment } from "@gram/client/models/components/risksegment.js";
import { mergeChunk, type WindowState } from "./useWindowedTranscript";

const message = (seq: number): ChatMessage =>
  ({ id: `m${seq}`, seq, role: "user" }) as ChatMessage;

const segment = (
  firstSeq: number,
  lastSeq: number,
  hasMoreAfter = true,
): RiskSegment => ({ firstSeq, lastSeq, hasMoreBefore: true, hasMoreAfter });

const chunk = (seqs: number[], segments: RiskSegment[]): Chat =>
  ({ messages: seqs.map(message), matchSegments: segments }) as Chat;

const window = (seqs: number[], gaps: number[] = []): WindowState => ({
  messages: seqs.map(message),
  gaps: new Set(gaps),
  hasMoreBefore: true,
  hasMoreAfter: true,
});

describe("mergeChunk", () => {
  it("joins a chunk whose first window shares a message with the loaded edge", () => {
    const next = mergeChunk(
      window([10, 11, 12]),
      chunk([12, 13, 14], [segment(12, 14)]),
    );
    expect(next.messages.map((m) => m.seq)).toEqual([10, 11, 12, 13, 14]);
    expect([...next.gaps]).toEqual([]);
  });

  it("opens a gap before a window past the loaded edge and between windows", () => {
    const next = mergeChunk(
      window([10, 11]),
      chunk([20, 21, 30], [segment(20, 21), segment(30, 30, false)]),
    );
    expect([...next.gaps].sort((a, b) => a - b)).toEqual([11, 21]);
    expect(next.hasMoreAfter).toBe(false);
  });

  it("leaves ranges the user already loaded alone", () => {
    const loaded = window([10, 11, 12, 13, 14, 15]);
    loaded.hasMoreAfter = false;
    const next = mergeChunk(
      loaded,
      chunk([11, 12, 14, 15], [segment(11, 12), segment(14, 15)]),
    );
    expect([...next.gaps]).toEqual([]);
    expect(next.hasMoreAfter).toBe(false);
  });

  it("keeps the window when a chunk has no matches", () => {
    const prev = window([10]);
    expect(mergeChunk(prev, chunk([], []))).toBe(prev);
  });

  it("seeds the window from the first chunk with matches", () => {
    const next = mergeChunk(
      window([]),
      chunk([5, 6, 9], [segment(5, 6), segment(9, 9)]),
    );
    expect(next.messages.map((m) => m.seq)).toEqual([5, 6, 9]);
    expect([...next.gaps]).toEqual([6]);
  });
});
