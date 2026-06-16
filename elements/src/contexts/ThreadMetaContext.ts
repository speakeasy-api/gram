import { createContext, useContext } from "react";

/**
 * Per-chat metadata that assistant-ui's thread list can't carry: its
 * RemoteThreadMetadata is a closed shape ({@link
 * https://github.com/Yonom/assistant-ui status/remoteId/externalId/title}), so
 * fields like the creation date are dropped at the runtime boundary.
 *
 * The Gram thread-list adapter populates this side channel from `chat.list`
 * (keyed by chat id, which equals the item's remoteId/externalId) and
 * `ThreadListItem` reads it to render the date. React context crosses the
 * shadow-root boundary the thread list renders into, so the provider lives up
 * in the Elements runtime tree.
 */
export interface ThreadMeta {
  /** ISO timestamp of when the chat was created. */
  createdAt?: string;
}

export const ThreadMetaContext = createContext<Record<string, ThreadMeta>>({});

/** Reads the metadata for one chat id, or undefined when unknown (e.g. a
 *  brand-new local thread that isn't in `chat.list` yet). */
export function useThreadMeta(id: string | undefined): ThreadMeta | undefined {
  const map = useContext(ThreadMetaContext);
  return id ? map[id] : undefined;
}
