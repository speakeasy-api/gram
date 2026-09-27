import { describe, expect, it } from "vitest";
import type { UserSession } from "@gram/client/models/components/usersession.js";
import { groupConnections } from "@/components/connections/groupConnections";
import { resolveSessionAgents } from "./session-agents";

const session = {
  id: "session-1",
  subjectType: "agent",
  subjectUrn: "agent:agent-1",
  issuerSlug: "example-server",
  createdAt: new Date(),
  updatedAt: new Date(),
  expiresAt: new Date(Date.now() + 3600000),
} as UserSession;

describe("resolveSessionAgents", () => {
  it("resolves readable agents to names and agent destinations, not identities", () => {
    const resolved = resolveSessionAgents(
      [session],
      [{ id: "agent-1", name: "Research agent" }],
    );
    const [group] = groupConnections(resolved, "subject");
    expect(group?.label).toBe("Research agent");
    expect(group?.identity).toEqual({ agentId: "agent-1" });
    expect(group?.sessions[0]?.subjectDisplayName).toBe("Research agent");
  });

  it("retains sessions without revealing names or links for unreadable/missing agents", () => {
    const previouslyResolved = {
      ...session,
      subjectDisplayName: "Old name",
      subjectAgentId: "agent-1",
    };
    const resolved = resolveSessionAgents(
      [previouslyResolved],
      [{ id: "other-agent", name: "Other agent" }],
    );
    const [group] = groupConnections(resolved, "subject");
    expect(group?.label).toBe("agent:agent-1");
    expect(group?.identity).toBeUndefined();
    expect(resolved[0]?.subjectAgentId).toBeUndefined();
    expect(group?.sessions).toHaveLength(1);
  });

  it("preserves user resolution and does not put agent links on provider groups", () => {
    const user = {
      ...session,
      subjectType: "user",
      subjectUrn: "user:user-1",
      subjectDisplayName: "Example User",
    };
    const resolved = resolveSessionAgents(
      [user, session],
      [{ id: "agent-1", name: "Research agent" }],
    );
    expect(resolved[0]).toBe(user);
    expect(
      groupConnections(resolved, "subject").find(
        (group) => group.key === user.subjectUrn,
      )?.identity,
    ).toEqual({ urn: user.subjectUrn });
    expect(
      groupConnections(resolved, "provider").every((group) => !group.identity),
    ).toBe(true);
  });
});
