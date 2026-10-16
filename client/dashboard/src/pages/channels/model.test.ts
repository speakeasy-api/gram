import { describe, expect, it } from "vitest";
import type { ChannelsSnapshot } from "./api";
import { emptyStore, messageReducer } from "./messages";
import {
  audienceOf,
  conversationsFrom,
  dashboardSessionIn,
  directThreadBetween,
  presenceGroups,
} from "./model";

const online = "2099-01-01T00:00:00Z";
const resolve = (subjectId: string) =>
  ({ "identity-1": "Avery Quinn", "identity-2": "Jordan Blake" })[subjectId];

function snapshotWith(overrides: Partial<ChannelsSnapshot>): ChannelsSnapshot {
  return {
    enabled: true,
    instances: {
      "session-1": {
        id: "session-1",
        subjectId: "identity-1",
        label: "avery on dashboard",
        note: "",
        origin: "dashboard",
        expiresAt: online,
      },
      "agent-1": {
        id: "agent-1",
        subjectId: "identity-1",
        label: "claude on laptop",
        note: "Fixing the flake",
        expiresAt: online,
      },
      "agent-2": {
        id: "agent-2",
        subjectId: "identity-1",
        label: "claude in CI",
        note: "",
        expiresAt: online,
      },
      expired: {
        id: "expired",
        subjectId: "identity-2",
        label: "claude on laptop",
        note: "",
        expiresAt: "2020-01-01T00:00:00Z",
      },
    },
    threads: {},
    messages: [],
    preferences: { followed: [], muted: [] },
    cursor: 0,
    ...overrides,
  };
}

/**
 * The page reads messages from what it has loaded, not from the snapshot, so
 * the list is built the way the page builds it: fold the snapshot's tail in
 * first.
 */
function listFrom(
  snapshot: ChannelsSnapshot,
  options: Parameters<typeof conversationsFrom>[1],
) {
  return conversationsFrom(snapshot, {
    ...options,
    store: messageReducer(emptyStore, { type: "snapshot", snapshot }),
  });
}

describe("conversation list", () => {
  const snapshot = snapshotWith({
    threads: {
      quiet: { id: "quiet", title: "Backlog grooming" },
      busy: { id: "busy", title: "Deploy" },
      dm: {
        id: "dm",
        title: "Direct message",
        participants: ["identity-1", "identity-1"],
        instanceIds: ["session-1", "agent-1"],
      },
    },
    messages: [
      {
        id: 1,
        threadId: "busy",
        instanceId: "agent-1",
        subjectId: "identity-1",
        text: "Shipping now",
        createdAt: "2026-01-01T10:00:00Z",
      },
      {
        id: 2,
        threadId: "dm",
        instanceId: "agent-1",
        subjectId: "identity-1",
        text: "Need a review",
        createdAt: "2026-01-01T11:00:00Z",
      },
    ],
    preferences: { followed: ["busy"], muted: ["dm"] },
  });

  it("puts live conversations above idle ones", () => {
    const conversations = listFrom(snapshot, {
      viewerId: "identity-2",
      resolve,
      readMarks: {},
    });
    expect(conversations.map((conversation) => conversation.id)).toEqual([
      "dm",
      "busy",
      "quiet",
    ]);
    expect(conversations[1]).toMatchObject({ followed: true, unread: 1 });
    expect(conversations[0]).toMatchObject({
      muted: true,
      private: true,
      // A read-all observer sees the conversation without being part of it.
      involvesViewer: false,
    });
    const [asParticipant] = listFrom(snapshot, {
      viewerId: "identity-1",
      resolve,
    });
    expect(asParticipant?.involvesViewer).toBe(true);
  });

  it("counts only what arrived after the viewer last looked, from someone else", () => {
    const [, deploy] = listFrom(snapshot, {
      viewerId: "identity-1",
      resolve,
      readMarks: {},
    });
    // The viewer's own agent posted it, so it is not unread for them.
    expect(deploy?.unread).toBe(0);
    const [, afterReading] = listFrom(snapshot, {
      viewerId: "identity-2",
      resolve,
      readMarks: { busy: 1 },
    });
    expect(afterReading?.unread).toBe(0);
  });

  it("names a private conversation after the other endpoint, not the account", () => {
    // Both endpoints belong to one account: only the labels tell them apart.
    const [direct] = listFrom(snapshot, {
      viewerId: "identity-1",
      resolve,
      sessionInstanceId: "session-1",
    });
    expect(direct?.title).toBe("claude on laptop");
    const [fromElsewhere] = listFrom(snapshot, {
      viewerId: "identity-1",
      resolve,
      sessionInstanceId: "agent-1",
    });
    expect(fromElsewhere?.title).toBe("avery on dashboard");
  });

  it("keeps naming a conversation after the agent once the viewer's session ends", () => {
    const ended = snapshotWith({
      // The browser session is no longer live, so it is not in presence.
      instances: {
        "agent-1": {
          id: "agent-1",
          subjectId: "identity-1",
          label: "claude on laptop",
          note: "",
          expiresAt: online,
        },
      },
      participants: {
        "session-1": {
          id: "session-1",
          subjectId: "identity-1",
          label: "avery on dashboard",
          origin: "dashboard",
        },
        "agent-1": {
          id: "agent-1",
          subjectId: "identity-1",
          label: "claude on laptop",
        },
      },
      threads: {
        dm: {
          id: "dm",
          title: "Direct message",
          participants: ["identity-1", "identity-1"],
          instanceIds: ["session-1", "agent-1"],
        },
      },
    });
    const [direct] = listFrom(ended, {
      viewerId: "identity-1",
      resolve,
    });
    expect(direct?.title).toBe("claude on laptop");
  });

  it("says who can read a conversation in plain words", () => {
    const [direct, deploy] = listFrom(snapshot, {
      viewerId: "identity-1",
      resolve,
      sessionInstanceId: "session-1",
    });
    expect(audienceOf(deploy!, snapshot, "identity-1", resolve)).toBe(
      "Everyone in your organization",
    );
    expect(audienceOf(direct!, snapshot, "identity-1", resolve)).toBe(
      "You, and admins who can read private conversations",
    );
  });
});

describe("presence", () => {
  it("groups live participants by person and puts the viewer first", () => {
    const snapshot = snapshotWith({});
    const groups = presenceGroups(snapshot, {
      viewerId: "identity-1",
      resolve,
    });
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({ owner: "You", isYou: true });
    expect(groups[0]!.entries.map((entry) => entry.name)).toEqual([
      "avery on dashboard",
      "claude in CI",
      "claude on laptop",
    ]);
    // An agent is not the person; a browser session is the person reading.
    expect(groups[0]!.entries.map((entry) => entry.isAgent)).toEqual([
      false,
      true,
      true,
    ]);
  });

  it("finds the viewer's own browser session and ignores agents", () => {
    const snapshot = snapshotWith({});
    expect(dashboardSessionIn(snapshot, "identity-1")?.id).toBe("session-1");
    expect(dashboardSessionIn(snapshot, "identity-2")).toBeUndefined();
  });

  it("reuses an existing private conversation between two endpoints", () => {
    const snapshot = snapshotWith({
      threads: {
        dm: {
          id: "dm",
          title: "Direct message",
          participants: ["identity-1", "identity-1"],
          instanceIds: ["session-1", "agent-1"],
        },
        reply: {
          id: "reply",
          title: "Branch",
          parentId: "dm",
          participants: ["identity-1", "identity-1"],
          instanceIds: ["session-1", "agent-1"],
        },
      },
    });
    expect(directThreadBetween(snapshot, "session-1", "agent-1")?.id).toBe(
      "dm",
    );
    expect(
      directThreadBetween(snapshot, "session-1", "agent-2"),
    ).toBeUndefined();
  });
});
