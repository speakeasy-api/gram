import { cleanup, render, renderHook, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import {
  auditActorPrincipal,
  auditPrincipalLabel,
  auditSubjectPrincipal,
  parseAuditPrincipal,
  useAuditPrincipals,
} from "./audit-principals";
import { AuditPrincipalLink } from "./principals";

const mocks = vi.hoisted(() => ({ agents: vi.fn(), members: vi.fn() }));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: mocks.agents,
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.members,
}));
vi.mock("@/components/agent-link", () => ({
  AgentIcon: () => <span aria-label="Agent" />,
  AgentLink: ({
    agentId,
    children,
  }: {
    agentId?: string;
    children: React.ReactNode;
  }) =>
    agentId ? (
      <a href={`/agents?id=${agentId}`}>{children}</a>
    ) : (
      <span>{children}</span>
    ),
}));
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({
    identifier,
    projectSlug,
    children,
  }: {
    identifier: { userId: string };
    projectSlug?: string;
    children: React.ReactNode;
  }) => (
    <a data-project={projectSlug} href={`/identities/${identifier.userId}`}>
      {children}
    </a>
  ),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
const log = (extra: Partial<AuditLog> = {}): AuditLog => ({
  id: "event",
  action: "user_session:revoke",
  actingSurface: "dashboard",
  actorType: "user",
  actorId: "revoker",
  actorDisplayName: "Administrator",
  subjectType: "user_session",
  subjectId: "session-id",
  subjectDisplayName: "user:target",
  createdAt: new Date(),
  ...extra,
});
function Row({ event }: { event: AuditLog }) {
  const identities = useAuditPrincipals([event]);
  return (
    <>
      <AuditPrincipalLink
        principal={auditActorPrincipal(event)}
        identities={identities}
        fallback={event.actorDisplayName!}
      />
      <AuditPrincipalLink
        principal={auditSubjectPrincipal(event)}
        identities={identities}
        fallback={event.subjectDisplayName!}
      />
    </>
  );
}

describe("audit principals", () => {
  it("memoizes inventories across feed rerenders and clears them on read errors", () => {
    mocks.agents.mockReturnValue({
      data: [{ id: "a", name: "Readable Agent" }],
    });
    mocks.members.mockReturnValue({
      data: { members: [{ id: "target", name: "Target Person" }] },
    });
    const logs = [log(), log({ subjectDisplayName: "agent:a" })];
    const { result, rerender } = renderHook(() => useAuditPrincipals(logs));
    const first = result.current;
    expect(mocks.agents).toHaveBeenCalledTimes(1);
    expect(mocks.members).toHaveBeenCalledTimes(1);
    rerender();
    expect(result.current).toBe(first);
    mocks.agents.mockReturnValue({
      data: [{ id: "a", name: "Stale Name" }],
      isError: true,
    });
    rerender();
    expect(result.current.agents.size).toBe(0);
  });

  it("highlights resolved actor names without exposing unauthorized agent names", () => {
    const renderLabel = vi.fn((label: string) => <mark>{label}</mark>);
    render(
      <AuditPrincipalLink
        principal={parseAuditPrincipal("agent:a")}
        identities={{ agents: new Map(), users: new Map() }}
        fallback="Private Name"
        renderLabel={renderLabel}
      />,
    );
    expect(renderLabel).toHaveBeenCalledWith("agent:a");
    expect(screen.queryByText("Private Name")).toBeNull();
    expect(screen.getByText("agent:a").tagName).toBe("MARK");
    expect(screen.getByTitle("agent:a")).toBeTruthy();
  });

  it("uses session ownership metadata, never session ID or revoker", () => {
    expect(auditSubjectPrincipal(log())).toEqual({
      kind: "user",
      id: "target",
      urn: "user:target",
    });
    expect(
      auditSubjectPrincipal(
        log({ metadata: { subject_urn: "agent:target-agent" } }),
      )?.urn,
    ).toBe("agent:target-agent");
    expect(
      auditSubjectPrincipal(log({ subjectDisplayName: undefined })),
    ).toBeNull();
    expect(auditActorPrincipal(log())?.id).toBe("revoker");
  });
  it("preserves colon-bearing user IDs and excludes legacy system actors", () => {
    expect(parseAuditPrincipal("user:provider:target")?.id).toBe(
      "provider:target",
    );
    expect(auditActorPrincipal(log({ actorId: "provider:target" }))?.id).toBe(
      "provider:target",
    );
    expect(
      auditSubjectPrincipal(
        log({ metadata: { subject_urn: "user:provider:target" } }),
      )?.id,
    ).toBe("provider:target");
    expect(auditActorPrincipal(log({ actorId: "system" }))).toBeNull();
    expect(auditActorPrincipal(log({ actorId: "user:system" }))).toBeNull();
  });
  it("uses the same readable principal labels for rendering and search", () => {
    const identities = {
      agents: new Map([["a", { id: "a", name: "Release agent" }]]),
      users: new Map(),
    } as ReturnType<typeof useAuditPrincipals>;
    expect(
      auditPrincipalLabel(
        parseAuditPrincipal("agent:a"),
        identities,
        "Someone",
      ),
    ).toBe("Release agent");
    expect(
      auditPrincipalLabel(
        parseAuditPrincipal("agent:unreadable"),
        identities,
        "Someone",
      ),
    ).toBe("agent:unreadable");
    expect(
      auditPrincipalLabel(
        parseAuditPrincipal("user:unknown"),
        identities,
        "User abc…",
      ),
    ).toBe("User abc…");
  });
  it("passes the row project to user identity links and retains formatted fallbacks", () => {
    render(
      <AuditPrincipalLink
        principal={parseAuditPrincipal("user:unknown")}
        identities={{ agents: new Map(), users: new Map() }}
        fallback="User abc…"
        projectSlug="other-project"
      />,
    );
    expect(
      screen
        .getByRole("link", { name: "User abc…" })
        .getAttribute("data-project"),
    ).toBe("other-project");
  });
  it("rejects malformed and unrelated references", () => {
    for (const raw of [null, {}, "agent:", "agent:a b", "key:abc"])
      expect(parseAuditPrincipal(raw)).toBeNull();
    expect(auditSubjectPrincipal(log({ subjectType: "api_key" }))).toBeNull();
    expect(auditActorPrincipal(log({ actorType: "api_key" }))).toBeNull();
  });
  it("accepts direct agents and prefixed actor IDs", () => {
    expect(
      auditSubjectPrincipal(log({ subjectType: "agent", subjectId: "a" }))?.urn,
    ).toBe("agent:a");
    expect(
      auditActorPrincipal(log({ actorType: "agent", actorId: "agent:a" }))?.id,
    ).toBe("a");
  });
  it("resolves target users separately from the human revoker in one member query", () => {
    mocks.agents.mockReturnValue({ data: [] });
    mocks.members.mockReturnValue({
      data: { members: [{ id: "target", name: "Target Person" }] },
    });
    render(<Row event={log()} />);
    expect(
      screen.getByRole("link", { name: "Target Person" }).getAttribute("href"),
    ).toBe("/identities/target");
    expect(
      screen.getByRole("link", { name: "Administrator" }).getAttribute("href"),
    ).toBe("/identities/revoker");
    expect(mocks.members).toHaveBeenCalledTimes(1);
  });
  it("links only readable agents and ignores stale names on errors", () => {
    mocks.members.mockReturnValue({ data: { members: [] } });
    mocks.agents.mockReturnValue({
      data: [{ id: "a", name: "Readable Agent" }],
    });
    const { rerender } = render(
      <Row event={log({ subjectDisplayName: "agent:a" })} />,
    );
    expect(
      screen.getByRole("link", { name: "Readable Agent" }).getAttribute("href"),
    ).toBe("/agents?id=a");
    mocks.agents.mockReturnValue({
      data: [{ id: "a", name: "Private Name" }],
      isError: true,
    });
    rerender(<Row event={log({ subjectDisplayName: "agent:a" })} />);
    expect(screen.queryByText("Private Name")).toBeNull();
    expect(screen.getByText("agent:a").closest("a")).toBeNull();
    mocks.agents.mockReturnValue({ data: [] });
    rerender(
      <Row
        event={log({
          subjectType: "agent",
          subjectId: "a",
          subjectDisplayName: "Old Secret",
        })}
      />,
    );
    expect(screen.queryByText("Old Secret")).toBeNull();
    expect(screen.getByText("agent:a").closest("a")).toBeNull();
  });
});
