import type {
  ChannelMessage,
  ChannelsDelta,
  ChannelsHistoryPage,
  ChannelsSnapshot,
} from "./api";

/**
 * What the viewer has loaded of one conversation. The server holds the archive
 * and hands out bounded pages; this is the part of it on screen.
 */
export interface ThreadHistory {
  /** Oldest first, no duplicates. */
  messages: ChannelMessage[];
  /** Older messages exist. Undefined until the server has been asked. */
  hasMore?: boolean;
  /** Where the page above this one starts. */
  nextBefore?: number;
  loading: boolean;
  /** Older messages are gone for good, not merely unloaded. */
  removedByRetention: boolean;
  /**
   * Why the last attempt to load older messages failed, when the viewer asked
   * for them. A background attempt that fails stays quiet and leaves the
   * conversation offering to try again.
   */
  error?: string;
}

/**
 * Every message the viewer has loaded, with how far back each conversation
 * reaches and how far forward the viewer has read.
 */
export interface MessageStore {
  /** The highest message seen: where the next incremental read starts. */
  cursor: number;
  byThread: Record<string, ThreadHistory>;
}

export const emptyStore: MessageStore = { cursor: 0, byThread: {} };

const NO_MESSAGES: ChannelMessage[] = [];
const UNREAD: ThreadHistory = {
  messages: NO_MESSAGES,
  loading: false,
  removedByRetention: false,
};

export function historyOf(
  store: MessageStore,
  threadId: string,
): ThreadHistory {
  return store.byThread[threadId] ?? UNREAD;
}

export function messagesIn(
  store: MessageStore,
  threadId: string,
): ChannelMessage[] {
  return store.byThread[threadId]?.messages ?? NO_MESSAGES;
}

/**
 * Folds messages into what is already held, keeping them ordered and unique.
 * Returns the array it was given when nothing is new, so a poll that brings
 * back what the viewer already has renders nothing again.
 */
export function mergeMessages(
  held: ChannelMessage[],
  arriving: ChannelMessage[],
): ChannelMessage[] {
  if (arriving.length === 0) return held;
  if (held.length === 0) return arriving;
  // A live read appends; paging backwards prepends. Both are whole pages that
  // sit entirely on one side of what is held, which is the common case.
  if (arriving[0]!.id > held[held.length - 1]!.id) return held.concat(arriving);
  if (arriving[arriving.length - 1]!.id < held[0]!.id)
    return arriving.concat(held);

  const unique = new Map<number, ChannelMessage>();
  for (const message of held) unique.set(message.id, message);
  let added = false;
  for (const message of arriving) {
    if (unique.has(message.id)) continue;
    unique.set(message.id, message);
    added = true;
  }
  if (!added) return held;
  return [...unique.values()].sort((a, b) => a.id - b.id);
}

export type MessageEvent =
  | { type: "snapshot"; snapshot: ChannelsSnapshot }
  | { type: "delta"; delta: ChannelsDelta }
  | { type: "loadingOlder"; threadId: string }
  | { type: "olderPage"; threadId: string; page: ChannelsHistoryPage }
  | { type: "olderFailed"; threadId: string; error?: string }
  | { type: "forget" };

function bucket(messages: ChannelMessage[]): Record<string, ChannelMessage[]> {
  const byThread: Record<string, ChannelMessage[]> = {};
  for (const message of messages) {
    const held = byThread[message.threadId];
    if (held) held.push(message);
    else byThread[message.threadId] = [message];
  }
  return byThread;
}

/** Folds a batch of messages in, leaving untouched conversations identical. */
function absorb(
  store: MessageStore,
  messages: ChannelMessage[],
  cursor: number,
): MessageStore {
  const arriving = bucket(messages);
  let byThread = store.byThread;
  for (const [threadId, batch] of Object.entries(arriving)) {
    const history = historyOf(store, threadId);
    const merged = mergeMessages(history.messages, batch);
    if (merged === history.messages) continue;
    if (byThread === store.byThread) byThread = { ...store.byThread };
    byThread[threadId] = { ...history, messages: merged };
  }
  const next = Math.max(store.cursor, cursor);
  if (byThread === store.byThread && next === store.cursor) return store;
  return { cursor: next, byThread };
}

export function messageReducer(
  store: MessageStore,
  event: MessageEvent,
): MessageStore {
  switch (event.type) {
    case "snapshot": {
      const { snapshot } = event;
      // Snapshot and delta requests race. A lagging snapshot is not evidence
      // of deleted history: only an explicit stale delta resets the cache.
      // Ignore it entirely so it cannot reintroduce rows after a retention reset.
      if (snapshot.cursor < store.cursor) return store;
      const absorbed = absorb(store, snapshot.messages, snapshot.cursor);
      // Nothing older than the tail exists anywhere, so the tail is every
      // readable message: each conversation is complete, and anything held
      // that the tail no longer carries was removed.
      if (snapshot.hasMore !== false) return absorbed;
      const surviving = bucket(snapshot.messages);
      let byThread = absorbed.byThread;
      const threadIds = new Set([
        ...Object.keys(snapshot.threads),
        ...Object.keys(absorbed.byThread),
      ]);
      for (const threadId of threadIds) {
        const history = historyOf(absorbed, threadId);
        const kept = surviving[threadId] ?? NO_MESSAGES;
        // Held messages are a superset of the tail's, so equal lengths mean
        // nothing was removed.
        const messages =
          kept.length === history.messages.length ? history.messages : kept;
        if (history.hasMore === false && messages === history.messages)
          continue;
        if (byThread === absorbed.byThread) byThread = { ...byThread };
        byThread[threadId] = {
          ...history,
          messages,
          hasMore: false,
          nextBefore: undefined,
        };
      }
      if (byThread === absorbed.byThread) return absorbed;
      return { ...absorbed, byThread };
    }
    case "delta": {
      const { delta } = event;
      // The viewer's cursor fell below retention: start again from the
      // snapshot rather than stitching onto messages that may be gone.
      if (delta.stale) return { ...emptyStore, cursor: delta.cursor };
      return absorb(store, delta.messages, delta.cursor);
    }
    case "loadingOlder": {
      const history = historyOf(store, event.threadId);
      if (history.loading) return store;
      return {
        ...store,
        byThread: {
          ...store.byThread,
          [event.threadId]: { ...history, loading: true, error: undefined },
        },
      };
    }
    case "olderPage": {
      const { page } = event;
      const absorbed = absorb(store, page.messages, store.cursor);
      const history = historyOf(absorbed, event.threadId);
      return {
        ...absorbed,
        byThread: {
          ...absorbed.byThread,
          [event.threadId]: {
            ...history,
            loading: false,
            error: undefined,
            hasMore: page.hasMore,
            nextBefore: page.hasMore ? page.nextBefore : undefined,
            removedByRetention:
              history.removedByRetention || page.stale === true,
          },
        },
      };
    }
    case "olderFailed": {
      const history = historyOf(store, event.threadId);
      return {
        ...store,
        byThread: {
          ...store.byThread,
          [event.threadId]: {
            ...history,
            loading: false,
            error: event.error,
          },
        },
      };
    }
    case "forget":
      return emptyStore;
  }
}
