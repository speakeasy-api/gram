import { getApiBaseURL, getServerURL } from "@/lib/utils";

/** Where a participant joined from. Stamped by the server, never the client. */
export const DASHBOARD_ORIGIN = "dashboard";

export interface ChannelInstance {
  id: string;
  subjectId: string;
  label: string;
  note: string;
  /** "dashboard" for a person reading here; empty for an agent over MCP. */
  origin?: string;
  expiresAt: string;
}
export interface ChannelThread {
  id: string;
  title: string;
  parentId?: string;
  participants?: string[];
  instanceIds?: string[];
  /**
   * The newest message in this conversation, so the list can preview a
   * conversation whose activity is older than the snapshot's tail. Absent on a
   * server that does not summarize threads; the newest loaded message is then
   * the best available answer.
   */
  lastMessage?: ChannelMessage;
  /** How many readable messages the conversation holds in total. */
  messageCount?: number;
}
export interface ChannelMessage {
  id: number;
  threadId: string;
  instanceId: string;
  subjectId: string;
  text: string;
  createdAt: string;
}
interface ChannelGrant {
  canConnect: boolean;
  canReadAll: boolean;
}
/** Display identity for anyone who has spoken, including ended sessions. */
export interface ChannelParticipant {
  id: string;
  subjectId: string;
  label: string;
  origin?: string;
}
/**
 * Sizes the server enforces. Read them from the snapshot rather than repeating
 * them here, so a limit the organization changes does not need a release.
 */
export interface ChannelsLimits {
  messageBytes: number;
  titleBytes: number;
  noteBytes: number;
  labelBytes: number;
  historyLimit: number;
  snapshotMessages: number;
}

export const DEFAULT_LIMITS: ChannelsLimits = {
  messageBytes: 32768,
  titleBytes: 256,
  noteBytes: 1024,
  labelBytes: 128,
  historyLimit: 200,
  snapshotMessages: 50,
};

export interface ChannelsSnapshot {
  enabled: boolean;
  grants?: Record<string, ChannelGrant>;
  /** Live sessions only: presence. */
  instances: Record<string, ChannelInstance>;
  /** Everyone who has spoken, so history keeps its names. */
  participants?: Record<string, ChannelParticipant>;
  threads: Record<string, ChannelThread>;
  /**
   * The most recent messages the viewer can read, oldest first — a bounded
   * summary, not the archive. Older messages are read per conversation.
   */
  messages: ChannelMessage[];
  /** True when messages older than this tail exist somewhere. */
  hasMore?: boolean;
  /** True when the organization has conversations beyond the ones here. */
  threadsHasMore?: boolean;
  /** Opaque keyset position for the next page of conversations. */
  threadsNextAfter?: string;
  limits?: Partial<ChannelsLimits>;
  preferences: { followed: string[] | null; muted: string[] | null };
  /** The highest message included here: where incremental reading starts. */
  cursor: number;
}

/** One page of a conversation's past, read backwards from `before`. */
export interface ChannelsHistoryPage {
  /** Oldest first, even though pages are requested newest first. */
  messages: ChannelMessage[];
  cursor: number;
  hasMore: boolean;
  /** Pass as the next `before`. Absent once the conversation is exhausted. */
  nextBefore?: number;
  /** The requested point is below the retention floor: it is gone for good. */
  stale?: boolean;
}

/**
 * One page of the organization's conversations, most recently active first.
 * `after` is opaque: it is the server's position, not an identifier to read.
 */
export interface ChannelsConversationsPage {
  /** Keyed by conversation id, exactly as the snapshot carries them. */
  threads: Record<string, ChannelThread>;
  hasMore: boolean;
  nextAfter?: string;
}

/** Everything that happened after a cursor, across every readable conversation. */
export interface ChannelsDelta {
  messages: ChannelMessage[];
  cursor: number;
  /** More is already waiting: read again now instead of waiting for the next poll. */
  hasMore?: boolean;
  /** The cursor predates retention; reload from the snapshot. */
  stale?: boolean;
}

/**
 * The browser session the dashboard speaks through. It carries no secret: the
 * server reclaims it for the signed-in account, so there is nothing to store.
 */
export interface DashboardSession {
  instanceId: string;
  label: string;
  note: string;
  expiresAt: string;
}

export class ChannelsRequestError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ChannelsRequestError";
    this.status = status;
  }
}

export async function channelsRequest<T>(
  projectSlug: string,
  operation?: string,
  args?: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(`${getApiBaseURL()}/channels/api`, {
    method: operation ? "POST" : "GET",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "Gram-Project": projectSlug,
    },
    body: operation
      ? JSON.stringify({ operation, arguments: args ?? {} })
      : undefined,
    signal,
  });
  if (!response.ok) {
    let detail = await response.text();
    try {
      const error: unknown = JSON.parse(detail);
      if (
        error &&
        typeof error === "object" &&
        "error" in error &&
        typeof error.error === "string"
      )
        detail = error.error;
    } catch {
      /* Non-JSON failures still have a useful status fallback. */
    }
    throw new ChannelsRequestError(
      detail || `Channels request failed (${response.status})`,
      response.status,
    );
  }
  return response.json() as Promise<T>;
}

/**
 * The endpoint an agent harness connects to. Agents reach the server directly,
 * never through the dashboard's development proxy.
 */
export function channelsMcpEndpoint(): string {
  return `${getServerURL()}/channels/mcp`;
}
