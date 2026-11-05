import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { ServerConnection } from "@/pages/agents/credentials/ownerConnections";
import { GrantOnBehalfOf } from "./GrantOnBehalfOf";
import { onBehalfOfPeople } from "./onBehalfOf";

const mocks = vi.hoisted(() => ({
  connection: undefined as ServerConnection | undefined,
}));
vi.mock("@/pages/agents/credentials/useServerInventory", () => ({
  useServerInventory: () => ({
    servers: [{ id: "server-1", resourceId: "toolset-1", name: "Grain" }],
  }),
}));
vi.mock("@/pages/agents/credentials/useOwnerConnections", () => ({
  useOwnerConnections: () => ({
    byServer: new Map(),
    assignedByServer: new Map(
      mocks.connection ? [["server-1", mocks.connection]] : [],
    ),
  }),
}));
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({
    identifier,
    children,
  }: {
    identifier: { userId: string };
    children: ReactNode;
  }) => <a href={`/identities/user:${identifier.userId}`}>{children}</a>,
}));
afterEach(cleanup);

/** A sign-in whose access token ran out, and nothing renews it. */
const LAPSED = { accessExpiresAt: new Date("2020-01-01T00:00:00Z") };

function session(fields: Partial<RemoteSession>): RemoteSession {
  return { subjectUrn: "user:user-ada", ...fields } as RemoteSession;
}

describe("onBehalfOfPeople", () => {
  it("names the owner of a person's own connection", () => {
    expect(
      onBehalfOfPeople({
        status: "ready",
        sessions: [
          session({
            subjectDisplayName: "Ada Lovelace",
            subjectEmail: "ada@example.com",
          }),
        ],
      }),
    ).toEqual([{ userId: "user-ada", name: "Ada Lovelace", expired: false }]);
  });

  it("falls back to the email, and marks a lapsed sign-in", () => {
    expect(
      onBehalfOfPeople({
        status: "ended",
        sessions: [session({ subjectEmail: "ada@example.com", ...LAPSED })],
      }),
    ).toEqual([{ userId: "user-ada", name: "ada@example.com", expired: true }]);
  });

  it("names one person once across their sign-ins", () => {
    expect(
      onBehalfOfPeople({
        status: "ended",
        sessions: [
          session({ subjectDisplayName: "Ada Lovelace" }),
          session({ subjectDisplayName: "Ada Lovelace", ...LAPSED }),
        ],
      }),
    ).toEqual([{ userId: "user-ada", name: "Ada Lovelace", expired: true }]);
  });

  it("names every owner, and marks only the one whose sign-in lapsed", () => {
    expect(
      onBehalfOfPeople({
        status: "ended",
        sessions: [
          session({ subjectDisplayName: "Ada Lovelace" }),
          session({
            subjectUrn: "user:user-bob",
            subjectDisplayName: "Bob Byte",
            ...LAPSED,
          }),
        ],
      }),
    ).toEqual([
      { userId: "user-ada", name: "Ada Lovelace", expired: false },
      { userId: "user-bob", name: "Bob Byte", expired: true },
    ]);
  });

  it.each([
    {
      case: "a shared credential",
      connection: { status: "not-required" as const },
    },
    {
      case: "a connection still being read",
      connection: { status: "checking" as const },
    },
    {
      case: "a missing connection",
      connection: { status: "missing" as const },
    },
    { case: "no connection", connection: undefined },
    {
      case: "a connection with no person behind it",
      connection: {
        status: "ready" as const,
        sessions: [session({ subjectUrn: "agent:agent-1" })],
      },
    },
  ])("acts for no one with $case", ({ connection }) => {
    expect(onBehalfOfPeople(connection)).toEqual([]);
  });
});

describe("GrantOnBehalfOf", () => {
  it("names the person whose connection the agent uses, linked to them", () => {
    mocks.connection = {
      status: "ready",
      sessions: [session({ subjectDisplayName: "Ada Lovelace" })],
    };
    render(<GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByText(/On behalf of/)).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Ada Lovelace" }).getAttribute("href"),
    ).toBe("/identities/user:user-ada");
    // Their face, drawn as initials when there is no photo.
    expect(screen.getByText("AL")).toBeTruthy();
    expect(screen.queryByText("(connection expired)")).toBeNull();
  });

  it("marks a connection that is assigned but has expired", () => {
    mocks.connection = {
      status: "ended",
      sessions: [session({ subjectDisplayName: "Ada Lovelace", ...LAPSED })],
    };
    render(<GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
    expect(screen.getByText("(connection expired)")).toBeTruthy();
  });

  it("names each person when the sign-ins belong to different people", () => {
    mocks.connection = {
      status: "ended",
      sessions: [
        session({ subjectDisplayName: "Ada Lovelace" }),
        session({
          subjectUrn: "user:user-bob",
          subjectDisplayName: "Bob Byte",
          ...LAPSED,
        }),
      ],
    };
    render(<GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />);
    expect(screen.getByRole("link", { name: "Ada Lovelace" })).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Bob Byte" }).getAttribute("href"),
    ).toBe("/identities/user:user-bob");
    // Only Bob's sign-in lapsed.
    expect(screen.getAllByText("(connection expired)")).toHaveLength(1);
  });

  it("shows nothing for a shared credential", () => {
    mocks.connection = { status: "not-required" };
    const { container } = render(
      <GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe("");
  });
});
