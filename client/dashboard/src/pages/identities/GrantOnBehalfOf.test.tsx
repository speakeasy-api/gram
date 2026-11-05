import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { ServerConnection } from "@/pages/agents/credentials/ownerConnections";
import { GrantOnBehalfOf, onBehalfOfPerson } from "./GrantOnBehalfOf";

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

function session(fields: Partial<RemoteSession>): RemoteSession {
  return { subjectUrn: "user:user-ada", ...fields } as RemoteSession;
}

describe("onBehalfOfPerson", () => {
  it("names the owner of a person's own connection", () => {
    expect(
      onBehalfOfPerson({
        status: "ready",
        sessions: [
          session({
            subjectDisplayName: "Ada Lovelace",
            subjectEmail: "ada@example.com",
          }),
        ],
      }),
    ).toEqual({ userId: "user-ada", name: "Ada Lovelace", expired: false });
  });

  it("falls back to the email, and marks an expired connection", () => {
    expect(
      onBehalfOfPerson({
        status: "ended",
        sessions: [session({ subjectEmail: "ada@example.com" })],
      }),
    ).toEqual({ userId: "user-ada", name: "ada@example.com", expired: true });
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
    expect(onBehalfOfPerson(connection)).toBeUndefined();
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
  });

  it("shows nothing for a shared credential", () => {
    mocks.connection = { status: "not-required" };
    const { container } = render(
      <GrantOnBehalfOf agentId="agent-1" resourceId="toolset-1" />,
    );
    expect(container.textContent).toBe("");
  });
});
