import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { useObservedAssistantActivity } from "./useObservedAssistantActivity";
import { FLEET_WINDOW_MS } from "./fleet-model";

afterEach(cleanup);
const now = Date.parse("2026-09-01T12:00:00Z");
const session: ChatOverview = {
  id: "capture",
  title: "Task",
  assistantId: "assistant",
  lastMessageTimestamp: new Date(now),
  createdAt: new Date(now),
  updatedAt: new Date(now),
  numMessages: 2,
};

it("retains only the latest explicit assistant timestamp across search/page changes, then ages it out", () => {
  const { result, rerender } = renderHook(
    ({ sessions, clock }) =>
      useObservedAssistantActivity("org/user/project", sessions, clock),
    { initialProps: { sessions: [session], clock: now } },
  );
  rerender({ sessions: [], clock: now + 1 });
  expect([...result.current]).toEqual([
    ["assistant", session.lastMessageTimestamp],
  ]);
  rerender({
    sessions: [
      { ...session, id: "older", lastMessageTimestamp: new Date(now - 60_000) },
      { ...session, id: "unbound", assistantId: undefined },
    ],
    clock: now + 2,
  });
  expect([...result.current]).toEqual([
    ["assistant", session.lastMessageTimestamp],
  ]);
  const newer = new Date(now + 60_000);
  rerender({
    sessions: [{ ...session, lastMessageTimestamp: newer }],
    clock: now + 60_000,
  });
  expect(result.current.get("assistant")).toEqual(newer);
  rerender({ sessions: [], clock: newer.getTime() + FLEET_WINDOW_MS });
  expect(result.current.get("assistant")).toEqual(newer);
  rerender({ sessions: [], clock: newer.getTime() + FLEET_WINDOW_MS + 1 });
  expect(result.current.size).toBe(0);
});

it.each(["other/user/project", "org/other/project", "org/user/other"])(
  "clears evidence when the organization/user/project context becomes %s",
  (context) => {
    const { result, rerender } = renderHook(
      ({ scope, sessions }) =>
        useObservedAssistantActivity(scope, sessions, now),
      { initialProps: { scope: "org/user/project", sessions: [session] } },
    );
    expect(result.current.size).toBe(1);
    rerender({ scope: context, sessions: [] });
    expect(result.current.size).toBe(0);
    rerender({ scope: "org/user/project", sessions: [] });
    expect(result.current.size).toBe(0);
  },
);
