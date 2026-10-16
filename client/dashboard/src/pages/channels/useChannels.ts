import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import {
  channelsRequest,
  ChannelsRequestError,
  DEFAULT_LIMITS,
  type ChannelsConversationsPage,
  type ChannelsDelta,
  type ChannelsHistoryPage,
  type ChannelsLimits,
  type ChannelsSnapshot,
  type ChannelThread,
  type DashboardSession,
} from "./api";
import { dashboardSessionIn, type ReadMarks } from "./model";
import {
  emptyStore,
  historyOf,
  messageReducer,
  type MessageStore,
} from "./messages";

/**
 * How often new messages are read. Channels has no push transport in the
 * dashboard yet, so the viewer reconciles forward from a cursor: each read
 * carries what arrived since the last one, not the conversation archive.
 */
export const POLL_INTERVAL_MS = 5_000;
/** Presence, conversation list and preferences move far more slowly. */
export const SNAPSHOT_INTERVAL_MS = 10_000;
/** One screenful or so of past messages per request. */
export const HISTORY_PAGE_SIZE = 50;
/** Conversations read per request when the list runs past the snapshot. */
export const CONVERSATION_PAGE_SIZE = 100;
/** The most a single catch-up read may carry; the server bounds it too. */
const DELTA_LIMIT = 200;
/** Long enough that a lost tab disappears on its own, short enough to notice. */
const LEASE_SECONDS = 300;
const HEARTBEAT_MS = 120_000;

export interface ChannelsAction {
  operation: string;
  args?: Record<string, unknown>;
  /** Operations that speak as the viewer need their browser session first. */
  withSession?: boolean;
}

export interface ChannelsWorkspace {
  data: ChannelsSnapshot | undefined;
  /** Messages the viewer has loaded, and how far back each conversation goes. */
  store: MessageStore;
  /** Sizes the server enforces, so the composer never invents its own. */
  limits: ChannelsLimits;
  /** True only before the first snapshot arrives, so refreshes stay quiet. */
  isLoading: boolean;
  isRefreshing: boolean;
  /** Set when a 401/403 means cached content must stop being shown. */
  accessDenied: boolean;
  error: Error | null;
  /** Reads are failing, but what is on screen is still worth showing. */
  reconnecting: boolean;
  /** When the last successful read landed, so staleness can be honest. */
  lastReadAt: number | undefined;
  isActing: boolean;
  canAdmin: boolean;
  canParticipate: boolean;
  sessionInstanceId: string | undefined;
  /** Runs an operation. Resolves to undefined on failure; `error` carries it. */
  run: (action: ChannelsAction) => Promise<unknown>;
  refresh: () => void;
  /** Loads a conversation's newest page the first time it is opened. */
  openConversation: (threadId: string) => void;
  /** Loads the page above what is held, and retries a failed one. */
  loadOlder: (threadId: string) => void;
  /** Whether the organization has conversations beyond the ones listed. */
  moreConversations: {
    hasMore: boolean;
    loading: boolean;
    error?: string;
  };
  /** Reads the next page of conversations, and retries a failed one. */
  loadMoreConversations: () => void;
}

/** Conversations read beyond the snapshot's first page. */
interface ConversationPages {
  threads: Record<string, ChannelThread>;
  /** Where the next page starts. Undefined once the list is exhausted. */
  nextAfter?: string;
  /** Undefined until a page has been read; the snapshot answers until then. */
  hasMore?: boolean;
  loading: boolean;
  error?: string;
}

const NO_PAGES: ConversationPages = { threads: {}, loading: false };

function revoked(error: unknown): boolean {
  return (
    error instanceof ChannelsRequestError &&
    (error.status === 401 || error.status === 403)
  );
}

/**
 * The Channels data layer: a bounded snapshot for discovery, a cursor-based
 * read for what arrives, per-conversation history for the past, one action
 * path, and the browser session the viewer speaks through — created or
 * reclaimed on demand rather than as a step the person has to take.
 */
export function useChannelsWorkspace(
  organizationId: string,
  userId: string,
): ChannelsWorkspace {
  const projectSlug = useProjectSlugForRequests();
  const queryClient = useQueryClient();
  const queryKey = useMemo(
    () => ["channels", organizationId, userId, projectSlug],
    [organizationId, userId, projectSlug],
  );

  const snapshot = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      channelsRequest<ChannelsSnapshot>(
        projectSlug,
        undefined,
        undefined,
        signal,
      ),
    refetchInterval: SNAPSHOT_INTERVAL_MS,
    staleTime: SNAPSHOT_INTERVAL_MS,
    // Refresh failures belong inline, not in the route's error boundary.
    throwOnError: false,
    retry: false,
  });

  const [store, dispatch] = useReducer(messageReducer, emptyStore);
  const [pages, setPages] = useState<ConversationPages>(NO_PAGES);
  const [readError, setReadError] = useState<Error | null>(null);
  const [lastReadAt, setLastReadAt] = useState<number>();

  // Revocation can be discovered by whichever read happens to run first, so
  // either one closes the page down.
  const accessDenied = revoked(snapshot.error) || revoked(readError);
  // A transient refresh failure can use cached data; revoked access cannot.
  const current = accessDenied ? undefined : snapshot.data;
  // The snapshot carries the most recently active conversations; pages read
  // past it fill in the rest. The snapshot's own copy of a conversation is
  // the fresher one, so it wins.
  //
  // This composition is load-bearing, not a convenience. Paging walks a keyset
  // over last activity, which moves: a conversation that receives a message
  // mid-walk jumps ahead of the cursor, and no later page will carry it. The
  // snapshot's first page is what brings it back. Trusting the pages alone
  // would lose exactly the conversations that just became interesting.
  const data = useMemo(() => {
    if (!current || Object.keys(pages.threads).length === 0) return current;
    return { ...current, threads: { ...pages.threads, ...current.threads } };
  }, [current, pages.threads]);
  const limits = useMemo(
    () => ({ ...DEFAULT_LIMITS, ...snapshot.data?.limits }),
    [snapshot.data?.limits],
  );

  // Messages are only ever read, so folding the snapshot's tail in is a
  // reconciliation: it heals a gap the cursor reads missed, and costs nothing
  // when there is none.
  useEffect(() => {
    if (snapshot.data) dispatch({ type: "snapshot", snapshot: snapshot.data });
  }, [snapshot.data]);

  useEffect(() => {
    if (accessDenied) {
      dispatch({ type: "forget" });
      setPages(NO_PAGES);
    }
  }, [accessDenied]);

  // The snapshot now carries every conversation, so pages read when it did
  // not would only keep conversations alive after they stopped existing.
  const snapshotIsComplete = snapshot.data?.threadsHasMore === false;
  useEffect(() => {
    if (snapshotIsComplete) setPages(NO_PAGES);
  }, [snapshotIsComplete]);

  const cursor = useRef(0);
  useEffect(() => {
    cursor.current = store.cursor;
  }, [store.cursor]);

  const refetchSnapshot = snapshot.refetch;
  const live = data?.enabled === true;
  // The retention floor the last stale read reported. Reacting to the same
  // floor twice cannot produce a different answer, and a snapshot and a read
  // that disagree about it would otherwise chase each other.
  const reportedFloor = useRef<number | undefined>(undefined);

  // Reading forward from a cursor, rather than re-reading the archive, is what
  // keeps this bounded as an organization's history grows. A read that reports
  // more already waiting is followed at once so catch-up is not paced behind
  // the poll interval.
  useEffect(() => {
    if (!live) return;
    let stopped = false;
    let timer = 0;
    // `read` returns a promise, so every schedule discards it explicitly.
    const later = (delay: number) => {
      timer = window.setTimeout(() => void read(), delay);
    };
    const read = async () => {
      if (document.hidden) {
        later(POLL_INTERVAL_MS);
        return;
      }
      try {
        const page = await channelsRequest<ChannelsDelta>(
          projectSlug,
          "channels_read",
          { cursor: cursor.current, limit: DELTA_LIMIT },
        );
        if (stopped) return;
        dispatch({
          type: "delta",
          delta: {
            messages: page.messages ?? [],
            cursor: page.cursor ?? cursor.current,
            hasMore: page.hasMore,
            stale: page.stale,
          },
        });
        // The cursor fell below retention: what is held may no longer exist,
        // so the snapshot has to re-establish the floor. Asking again for a
        // floor already asked about would only repeat the exchange.
        if (page.stale && reportedFloor.current !== page.cursor) {
          reportedFloor.current = page.cursor;
          void refetchSnapshot();
        }
        if (!page.stale) reportedFloor.current = undefined;
        setReadError(null);
        setLastReadAt(Date.now());
        // "More is waiting" is only worth chasing if the cursor moved, and a
        // stale response is a reset rather than a backlog. Either read at
        // once would otherwise be a tight loop.
        const advanced = (page.cursor ?? 0) > cursor.current;
        later(page.hasMore && advanced && !page.stale ? 0 : POLL_INTERVAL_MS);
      } catch (failure) {
        if (stopped) return;
        setReadError(failure as Error);
        later(POLL_INTERVAL_MS);
      }
    };
    const catchUp = () => {
      if (document.hidden) return;
      window.clearTimeout(timer);
      later(0);
    };
    document.addEventListener("visibilitychange", catchUp);
    later(POLL_INTERVAL_MS);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", catchUp);
    };
  }, [projectSlug, live, refetchSnapshot]);

  // One history request per conversation at a time: scrolling to the top
  // while a page is still arriving must not ask for the same page twice.
  const loading = useRef(new Set<string>());
  const loadPage = useCallback(
    async (threadId: string, before?: number, asked = true) => {
      if (loading.current.has(threadId)) return;
      loading.current.add(threadId);
      dispatch({ type: "loadingOlder", threadId });
      try {
        const page = await channelsRequest<ChannelsHistoryPage>(
          projectSlug,
          "channels_history",
          {
            threadId,
            limit: HISTORY_PAGE_SIZE,
            ...(before === undefined ? {} : { before }),
          },
        );
        dispatch({
          type: "olderPage",
          threadId,
          page: { ...page, messages: page.messages ?? [] },
        });
      } catch (failure) {
        // Opening a conversation reconciles its history in the background,
        // and the tail is already on screen. Only a page the viewer asked for
        // is worth reporting; the rest leaves the offer to load it in place.
        dispatch({
          type: "olderFailed",
          threadId,
          ...(asked ? { error: (failure as Error).message } : {}),
        });
      } finally {
        loading.current.delete(threadId);
      }
    },
    [projectSlug],
  );

  // Effects and scroll handlers call these on every render and every scroll
  // event, so they read the live store from a ref rather than closing over it.
  const held = useRef(store);
  useEffect(() => {
    held.current = store;
  }, [store]);

  const openConversation = useCallback(
    (threadId: string) => {
      if (!threadId) return;
      const history = historyOf(held.current, threadId);
      // `undefined` means this conversation has never been asked about: the
      // snapshot's tail is a summary, not a complete conversation.
      if (history.hasMore !== undefined || history.loading) return;
      void loadPage(threadId, undefined, false);
    },
    [loadPage],
  );

  const loadOlder = useCallback(
    (threadId: string) => {
      if (!threadId) return;
      const history = historyOf(held.current, threadId);
      if (history.loading || history.removedByRetention) return;
      if (history.hasMore === undefined) {
        void loadPage(threadId);
        return;
      }
      if (!history.hasMore || history.nextBefore === undefined) return;
      void loadPage(threadId, history.nextBefore);
    },
    [loadPage],
  );

  const nextAfter = pages.nextAfter ?? current?.threadsNextAfter;
  // The in-flight guard is a ref, not the rendered `loading`: a state updater
  // may be called more than once for one update, and a request is not
  // something that may happen twice.
  const readingConversations = useRef(false);
  const loadMoreConversations = useCallback(() => {
    if (!nextAfter || readingConversations.current) return;
    readingConversations.current = true;
    setPages((held) => ({ ...held, loading: true, error: undefined }));
    void channelsRequest<ChannelsConversationsPage>(
      projectSlug,
      "channels_conversations",
      { after: nextAfter, limit: CONVERSATION_PAGE_SIZE },
    )
      .then((page) => {
        setPages((held) => ({
          threads: { ...held.threads, ...page.threads },
          nextAfter: page.hasMore ? page.nextAfter : undefined,
          hasMore: page.hasMore,
          loading: false,
        }));
      })
      .catch((failure: Error) => {
        setPages((held) => ({
          ...held,
          loading: false,
          error: failure.message,
        }));
      })
      .finally(() => {
        readingConversations.current = false;
      });
  }, [projectSlug, nextAfter]);

  // The session the server last handed back, with the lease it came with.
  // The polled snapshot is the real source of truth; this covers the gap
  // before the next poll lands, which is several sends wide.
  const [opened, setOpened] = useState<{ id: string; expiresAt: number }>();
  // Only a session the organization can still see is safe to speak through:
  // an abandoned lease has to be reclaimed before it can post again. A lease
  // the server just issued counts until it runs out.
  const liveSessionId = dashboardSessionIn(data, userId)?.id;
  const heldSessionId =
    opened && opened.expiresAt > Date.now() ? opened.id : undefined;
  const sessionInstanceId = liveSessionId ?? heldSessionId;

  // Simultaneous sends, or a send racing a reload, must not each open a
  // session. One request is in flight at a time; the server itself reclaims.
  const opening = useRef<Promise<string> | null>(null);
  const ensureSession = useCallback(() => {
    opening.current ??= channelsRequest<DashboardSession>(
      projectSlug,
      "channels_dashboard_session",
      { leaseSeconds: LEASE_SECONDS },
    )
      .then((session) => {
        setOpened({
          id: session.instanceId,
          expiresAt: Date.parse(session.expiresAt),
        });
        return session.instanceId;
      })
      .finally(() => {
        opening.current = null;
      });
    return opening.current;
  }, [projectSlug]);

  const action = useMutation({
    mutationFn: async ({ operation, args, withSession }: ChannelsAction) => {
      const speaker = withSession
        ? { instanceId: sessionInstanceId ?? (await ensureSession()) }
        : undefined;
      return channelsRequest<unknown>(projectSlug, operation, {
        ...speaker,
        ...args,
      });
    },
    throwOnError: false,
    retry: false,
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });

  // Renewing the lease keeps the viewer visible to the organization while they
  // read. A failure here is not worth reporting: the next poll will show it.
  useEffect(() => {
    if (!sessionInstanceId || !live) return;
    const renew = () => {
      if (document.hidden) return;
      void channelsRequest(projectSlug, "channels_presence", {
        instanceId: sessionInstanceId,
        leaseSeconds: LEASE_SECONDS,
      }).catch(() => {});
    };
    const timer = window.setInterval(renew, HEARTBEAT_MS);
    return () => window.clearInterval(timer);
  }, [sessionInstanceId, live, projectSlug]);

  return {
    data,
    store: accessDenied ? emptyStore : store,
    limits,
    isLoading: snapshot.isPending && !data,
    isRefreshing: snapshot.isFetching,
    accessDenied,
    // The read that discovered revocation is the one that can explain it, so
    // it stays reportable even though its content is withheld.
    error: action.error ?? snapshot.error ?? readError,
    reconnecting: !accessDenied && (readError !== null || snapshot.isError),
    lastReadAt,
    isActing: action.isPending,
    canAdmin: data?.grants !== undefined,
    canParticipate: live,
    sessionInstanceId,
    run: async (next) => {
      try {
        return await action.mutateAsync(next);
      } catch {
        // Failures are reported inline through `error`, and a failed send must
        // leave the composer's draft alone, so callers branch on the result.
        return undefined;
      }
    },
    refresh: () => {
      setReadError(null);
      void snapshot.refetch();
    },
    openConversation,
    loadOlder,
    moreConversations: {
      hasMore: (pages.hasMore ?? current?.threadsHasMore ?? false) === true,
      loading: pages.loading,
      error: pages.error,
    },
    loadMoreConversations,
  };
}

/**
 * Remembers how far the viewer has read in each conversation so returning to
 * the page shows what moved while they were away.
 */
export function useReadMarks(
  storageKey: string,
): [ReadMarks, (threadId: string, messageId: number) => void] {
  const [marks, setMarks] = useState<ReadMarks>(() => {
    try {
      const stored: unknown = JSON.parse(
        localStorage.getItem(storageKey) ?? "{}",
      );
      if (stored && typeof stored === "object") return stored as ReadMarks;
    } catch {
      /* Storage can be unavailable in a restricted browser. */
    }
    return {};
  });

  const markRead = useCallback(
    (threadId: string, messageId: number) => {
      setMarks((current) => {
        if ((current[threadId] ?? 0) >= messageId) return current;
        const next = { ...current, [threadId]: messageId };
        try {
          localStorage.setItem(storageKey, JSON.stringify(next));
        } catch {
          /* Read state is a convenience, not data worth failing over. */
        }
        return next;
      });
    },
    [storageKey],
  );

  return [marks, markRead];
}
