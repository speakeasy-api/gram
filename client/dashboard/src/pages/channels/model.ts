import {
  DASHBOARD_ORIGIN,
  type ChannelInstance,
  type ChannelParticipant,
  type ChannelMessage,
  type ChannelsSnapshot,
  type ChannelThread,
} from "./api";
import { emptyStore, messagesIn, type MessageStore } from "./messages";

/** What the viewer has already read, keyed by conversation. */
export type ReadMarks = Record<string, number>;

export type ConversationKind = "thread" | "direct";

export interface Conversation {
  id: string;
  /** Title in the viewer's words: a topic, or who they are talking to. */
  title: string;
  kind: ConversationKind;
  /** Reply conversations hang off a parent and read as part of it. */
  parentId?: string;
  private: boolean;
  /** True when the viewer's own account is party to a private conversation. */
  involvesViewer: boolean;
  followed: boolean;
  muted: boolean;
  messages: ChannelMessage[];
  lastMessage?: ChannelMessage;
  lastActivity: number;
  unread: number;
}

export interface Speaker {
  /** Who is talking: the agent or browser session that posted. */
  name: string;
  /** The person the session belongs to, in the viewer's words. */
  owner: string;
  /** The account's own name, used where "You" would make a poor label. */
  account: string;
  isAgent: boolean;
  isYou: boolean;
}

export interface PresenceEntry {
  instance: ChannelInstance;
  name: string;
  isAgent: boolean;
  online: boolean;
}

export interface PresenceGroup {
  subjectId: string;
  owner: string;
  isYou: boolean;
  entries: PresenceEntry[];
}

/** Resolves an account to the name a person recognizes. */
export type ResolveOwner = (subjectId: string) => string | undefined;

const UNKNOWN_OWNER = "Former member";
const UNNAMED_AGENT = "Unnamed agent";

export function isAgent(
  participant: ChannelInstance | ChannelParticipant | undefined,
): boolean {
  return participant?.origin !== DASHBOARD_ORIGIN;
}

/** A session's display identity, whether or not its lease is still live. */
function participantOf(
  snapshot: ChannelsSnapshot,
  instanceId: string,
): ChannelInstance | ChannelParticipant | undefined {
  return snapshot.instances[instanceId] ?? snapshot.participants?.[instanceId];
}

export function isOnline(instance: ChannelInstance, now = Date.now()): boolean {
  return Date.parse(instance.expiresAt) > now;
}

function nameOf(
  participant: ChannelInstance | ChannelParticipant | undefined,
): string {
  if (!participant) return UNNAMED_AGENT;
  return participant.label.trim() || UNNAMED_AGENT;
}

function ownerName(
  subjectId: string,
  viewerId: string,
  resolve: ResolveOwner,
): string {
  if (subjectId === viewerId) return "You";
  return resolve(subjectId)?.trim() || UNKNOWN_OWNER;
}

/**
 * Names a private conversation after the other side of it. Two sessions of the
 * same account can talk to each other, so the endpoint label — not the
 * account — is what tells those conversations apart. An endpoint that has since
 * disconnected keeps its name, and a conversation from before names were kept
 * falls back to the people in it.
 */
function directTitle(
  thread: ChannelThread,
  snapshot: ChannelsSnapshot,
  options: {
    viewerId: string;
    sessionInstanceId?: string;
    resolve: ResolveOwner;
  },
): string {
  const endpoints = thread.instanceIds ?? [];
  const others = endpoints.filter((id) => id !== options.sessionInstanceId);
  // The viewer's own browser session does not name a conversation, even once
  // its lease has ended and it is no longer the session they hold — unless it
  // is the only other side, which is the viewer talking to themselves.
  const theirs = others.filter((id) => {
    const participant = participantOf(snapshot, id);
    return !(
      participant?.origin === DASHBOARD_ORIGIN &&
      participant.subjectId === options.viewerId
    );
  });
  const named = (
    theirs.length > 0 ? theirs : others.length > 0 ? others : endpoints
  )
    .filter((id) => participantOf(snapshot, id))
    .map((id) => nameOf(participantOf(snapshot, id)));
  if (named.length > 0) return named.join(" and ");

  const owners = [...new Set(thread.participants ?? [])];
  const people = (
    owners.length > 1
      ? owners.filter((subjectId) => subjectId !== options.viewerId)
      : owners
  ).map((subjectId) => ownerName(subjectId, options.viewerId, options.resolve));
  return people.length > 0 ? people.join(" and ") : "Direct message";
}

export function speakerFor(
  message: ChannelMessage,
  snapshot: ChannelsSnapshot,
  viewerId: string,
  resolve: ResolveOwner,
): Speaker {
  const participant = participantOf(snapshot, message.instanceId);
  return {
    name: nameOf(participant),
    owner: ownerName(message.subjectId, viewerId, resolve),
    account: resolve(message.subjectId)?.trim() ?? "",
    isAgent: isAgent(participant),
    isYou: message.subjectId === viewerId,
  };
}

/**
 * Builds the conversation list the sidebar reads from: newest activity first,
 * with the conversations that have never been used last so an idle thread
 * never outranks a live one.
 */
export function conversationsFrom(
  snapshot: ChannelsSnapshot,
  options: {
    viewerId: string;
    resolve: ResolveOwner;
    sessionInstanceId?: string;
    readMarks?: ReadMarks;
    /** What the viewer has loaded. Only the pane needs the messages. */
    store?: MessageStore;
  },
): Conversation[] {
  const store = options.store ?? emptyStore;
  const followed = snapshot.preferences.followed ?? [];
  const muted = snapshot.preferences.muted ?? [];
  const readMarks = options.readMarks ?? {};

  const conversations = Object.values(snapshot.threads).map<Conversation>(
    (thread) => {
      const messages = messagesIn(store, thread.id);
      // The server summarizes each conversation's newest message so a
      // conversation whose activity predates the loaded pages still previews.
      const lastMessage = newestOf(
        messages[messages.length - 1],
        thread.lastMessage,
      );
      const isPrivate = (thread.participants?.length ?? 0) > 0;
      const mark = readMarks[thread.id] ?? 0;
      return {
        id: thread.id,
        title: isPrivate
          ? directTitle(thread, snapshot, options)
          : thread.title.trim() || "Untitled thread",
        kind: isPrivate ? "direct" : "thread",
        parentId: thread.parentId,
        private: isPrivate,
        involvesViewer: (thread.participants ?? []).includes(options.viewerId),
        followed: followed.includes(thread.id),
        muted: muted.includes(thread.id),
        messages,
        lastMessage,
        lastActivity: lastMessage ? Date.parse(lastMessage.createdAt) : 0,
        unread: unreadCount(messages, lastMessage, mark, options.viewerId),
      };
    },
  );

  return conversations.sort((a, b) => {
    if (a.lastActivity !== b.lastActivity)
      return b.lastActivity - a.lastActivity;
    return a.title.localeCompare(b.title);
  });
}

function newestOf(
  ...candidates: (ChannelMessage | undefined)[]
): ChannelMessage | undefined {
  let newest: ChannelMessage | undefined;
  for (const candidate of candidates) {
    if (candidate && (!newest || candidate.id > newest.id)) newest = candidate;
  }
  return newest;
}

/**
 * How much arrived since the viewer last looked. Only loaded messages can be
 * counted exactly; a summary newer than the loaded pages still has to show
 * that something is waiting, so it counts for one.
 */
function unreadCount(
  messages: ChannelMessage[],
  newest: ChannelMessage | undefined,
  mark: number,
  viewerId: string,
): number {
  let unread = 0;
  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index]!;
    if (message.id <= mark) break;
    if (message.subjectId !== viewerId) unread++;
  }
  if (unread > 0) return unread;
  const unloaded =
    newest &&
    newest.id > mark &&
    newest.subjectId !== viewerId &&
    newest.id > (messages[messages.length - 1]?.id ?? 0);
  return unloaded ? 1 : 0;
}

/** Groups live participants by the person who owns them, viewer first. */
export function presenceGroups(
  snapshot: ChannelsSnapshot,
  options: { viewerId: string; resolve: ResolveOwner; now?: number },
): PresenceGroup[] {
  const now = options.now ?? Date.now();
  const groups = new Map<string, PresenceGroup>();
  for (const instance of Object.values(snapshot.instances)) {
    if (!isOnline(instance, now)) continue;
    const group = groups.get(instance.subjectId) ?? {
      subjectId: instance.subjectId,
      owner: ownerName(instance.subjectId, options.viewerId, options.resolve),
      isYou: instance.subjectId === options.viewerId,
      entries: [],
    };
    group.entries.push({
      instance,
      name: nameOf(instance),
      isAgent: isAgent(instance),
      online: true,
    });
    groups.set(instance.subjectId, group);
  }
  for (const group of groups.values()) {
    group.entries.sort((a, b) => a.name.localeCompare(b.name));
  }
  return [...groups.values()].sort((a, b) => {
    if (a.isYou !== b.isYou) return a.isYou ? -1 : 1;
    return a.owner.localeCompare(b.owner);
  });
}

/** The viewer's live browser session, if the organization can still see it. */
export function dashboardSessionIn(
  snapshot: ChannelsSnapshot | undefined,
  viewerId: string,
  now = Date.now(),
): ChannelInstance | undefined {
  if (!snapshot) return undefined;
  return Object.values(snapshot.instances)
    .filter(
      (instance) =>
        instance.subjectId === viewerId &&
        instance.origin === DASHBOARD_ORIGIN &&
        isOnline(instance, now),
    )
    .sort((a, b) => Date.parse(b.expiresAt) - Date.parse(a.expiresAt))[0];
}

/**
 * An existing private conversation between these two endpoints. Reusing it
 * keeps a person from stacking up duplicate conversations with one agent.
 */
export function directThreadBetween(
  snapshot: ChannelsSnapshot,
  mine: string,
  theirs: string,
): ChannelThread | undefined {
  return Object.values(snapshot.threads).find(
    (thread) =>
      !thread.parentId &&
      (thread.participants?.length ?? 0) > 0 &&
      thread.instanceIds?.length === 2 &&
      thread.instanceIds.includes(mine) &&
      thread.instanceIds.includes(theirs),
  );
}

/** Plain-language answer to "who can read this?". */
export function audienceOf(
  conversation: Conversation,
  snapshot: ChannelsSnapshot,
  viewerId: string,
  resolve: ResolveOwner,
): string {
  if (!conversation.private) return "Everyone in your organization";
  const thread = snapshot.threads[conversation.id];
  const owners = [...new Set(thread?.participants ?? [])].map((subjectId) =>
    ownerName(subjectId, viewerId, resolve),
  );
  const people = owners.length > 0 ? owners.join(" and ") : "its participants";
  return `${people}, and admins who can read private conversations`;
}
