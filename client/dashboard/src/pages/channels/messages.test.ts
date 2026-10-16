import { describe, expect, it } from "vitest";
import type { ChannelMessage, ChannelsSnapshot } from "./api";
import {
  emptyStore,
  historyOf,
  mergeMessages,
  messageReducer,
  messagesIn,
} from "./messages";

function message(id: number, threadId = "thread-1"): ChannelMessage {
  return {
    id,
    threadId,
    instanceId: "instance-1",
    subjectId: "identity-1",
    text: `Message ${id}`,
    createdAt: "2026-01-01T00:00:00Z",
  };
}

function snapshotOf(
  messages: ChannelMessage[],
  overrides: Partial<ChannelsSnapshot> = {},
): ChannelsSnapshot {
  return {
    enabled: true,
    instances: {},
    threads: { "thread-1": { id: "thread-1", title: "Planning" } },
    messages,
    cursor: messages[messages.length - 1]?.id ?? 0,
    preferences: { followed: [], muted: [] },
    ...overrides,
  };
}

describe("merging messages", () => {
  it("keeps the array it was given when a read brings back nothing new", () => {
    const held = [message(1), message(2)];
    expect(mergeMessages(held, [])).toBe(held);
    expect(mergeMessages(held, [message(1), message(2)])).toBe(held);
  });

  it("appends what arrived and prepends what was paged back", () => {
    const held = [message(5), message(6)];
    expect(mergeMessages(held, [message(7)]).map((m) => m.id)).toEqual([
      5, 6, 7,
    ]);
    expect(
      mergeMessages(held, [message(3), message(4)]).map((m) => m.id),
    ).toEqual([3, 4, 5, 6]);
  });

  it("folds an overlapping page in once, in order", () => {
    // A retried read and a history page can cover the same messages. Showing
    // one of them twice would be a visible defect.
    const merged = mergeMessages(
      [message(3), message(4), message(7)],
      [message(2), message(4), message(5), message(7)],
    );
    expect(merged.map((m) => m.id)).toEqual([2, 3, 4, 5, 7]);
  });
});

describe("message store", () => {
  it("separates conversations and leaves untouched ones identical", () => {
    const seeded = messageReducer(emptyStore, {
      type: "snapshot",
      snapshot: snapshotOf([message(1), message(2, "thread-2")]),
    });
    expect(messagesIn(seeded, "thread-1").map((m) => m.id)).toEqual([1]);
    expect(messagesIn(seeded, "thread-2").map((m) => m.id)).toEqual([2]);

    const after = messageReducer(seeded, {
      type: "delta",
      delta: { messages: [message(3, "thread-2")], cursor: 3 },
    });
    // Re-rendering a conversation nothing happened in is wasted work.
    expect(historyOf(after, "thread-1")).toBe(historyOf(seeded, "thread-1"));
    expect(after.cursor).toBe(3);
  });

  it("treats the whole tail as complete when nothing older exists", () => {
    const seeded = messageReducer(emptyStore, {
      type: "snapshot",
      snapshot: snapshotOf([message(1)], { hasMore: false }),
    });
    // Knowing this saves one history request per conversation opened.
    expect(historyOf(seeded, "thread-1").hasMore).toBe(false);
  });

  it("leaves a conversation unasked when older messages may exist", () => {
    const seeded = messageReducer(emptyStore, {
      type: "snapshot",
      snapshot: snapshotOf([message(40)], { hasMore: true }),
    });
    expect(historyOf(seeded, "thread-1").hasMore).toBeUndefined();
  });

  it("ignores a lagging snapshot after newer messages and paged scrollback", () => {
    let held = messageReducer(emptyStore, {
      type: "snapshot",
      snapshot: snapshotOf([message(9), message(10)]),
    });
    held = messageReducer(held, {
      type: "olderPage",
      threadId: "thread-1",
      page: {
        messages: [message(1), message(2)],
        cursor: 10,
        hasMore: true,
        nextBefore: 1,
      },
    });
    held = messageReducer(held, {
      type: "delta",
      delta: { messages: [message(11)], cursor: 11 },
    });
    const result = messageReducer(held, {
      type: "snapshot",
      snapshot: snapshotOf([message(9), message(10)]),
    });
    expect(result).toBe(held);
    expect(messagesIn(result, "thread-1").map((m) => m.id)).toEqual([
      1, 2, 9, 10, 11,
    ]);
    expect(result.cursor).toBe(11);
    expect(historyOf(result, "thread-1").nextBefore).toBe(1);
  });

  it("drops what it holds when its cursor falls below retention", () => {
    const held = messageReducer(emptyStore, {
      type: "snapshot",
      snapshot: snapshotOf([message(1), message(2)]),
    });
    const reset = messageReducer(held, {
      type: "delta",
      delta: { messages: [], cursor: 500, stale: true },
    });
    expect(messagesIn(reset, "thread-1")).toHaveLength(0);
    expect(reset.cursor).toBe(500);
  });

  it("records where the next page of a conversation starts", () => {
    const loading = messageReducer(emptyStore, {
      type: "loadingOlder",
      threadId: "thread-1",
    });
    expect(historyOf(loading, "thread-1").loading).toBe(true);

    const paged = messageReducer(loading, {
      type: "olderPage",
      threadId: "thread-1",
      page: {
        messages: [message(10), message(11)],
        cursor: 11,
        hasMore: true,
        nextBefore: 10,
      },
    });
    expect(historyOf(paged, "thread-1")).toMatchObject({
      loading: false,
      hasMore: true,
      nextBefore: 10,
    });

    const exhausted = messageReducer(paged, {
      type: "olderPage",
      threadId: "thread-1",
      page: { messages: [message(9)], cursor: 9, hasMore: false },
    });
    expect(historyOf(exhausted, "thread-1")).toMatchObject({
      hasMore: false,
      nextBefore: undefined,
    });
    expect(messagesIn(exhausted, "thread-1").map((m) => m.id)).toEqual([
      9, 10, 11,
    ]);
  });

  it("keeps the oldest retained page while reporting that more is gone", () => {
    // The usual shape of a retention-bounded page carries messages: it is the
    // oldest page still held, not an empty refusal. Dropping them would lose
    // the oldest history the organization still has.
    const paged = messageReducer(emptyStore, {
      type: "olderPage",
      threadId: "thread-1",
      page: {
        messages: [message(10), message(11)],
        cursor: 11,
        hasMore: false,
        stale: true,
      },
    });
    expect(messagesIn(paged, "thread-1").map((m) => m.id)).toEqual([10, 11]);
    expect(historyOf(paged, "thread-1")).toMatchObject({
      removedByRetention: true,
      hasMore: false,
      nextBefore: undefined,
    });
  });

  it("keeps a failed page retryable without losing what is held", () => {
    const held = messageReducer(emptyStore, {
      type: "olderPage",
      threadId: "thread-1",
      page: {
        messages: [message(10)],
        cursor: 10,
        hasMore: true,
        nextBefore: 10,
      },
    });
    const failed = messageReducer(held, {
      type: "olderFailed",
      threadId: "thread-1",
      error: "Channels is briefly unavailable",
    });
    expect(historyOf(failed, "thread-1")).toMatchObject({
      loading: false,
      error: "Channels is briefly unavailable",
      hasMore: true,
      nextBefore: 10,
    });
    expect(messagesIn(failed, "thread-1")).toHaveLength(1);
  });

  it("forgets everything when access is withdrawn", () => {
    const held = messageReducer(emptyStore, {
      type: "snapshot",
      snapshot: snapshotOf([message(1)]),
    });
    expect(messageReducer(held, { type: "forget" })).toBe(emptyStore);
  });
});
