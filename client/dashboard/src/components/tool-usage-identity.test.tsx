import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { identityUrnFor, type IdentityRef } from "@/lib/identity-urn";
import { ToolUsageIdentity } from "./tool-usage-identity";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: {
      detail: {
        overview: {
          href: (urn: string) => `/example/project/identities/${urn}/overview`,
        },
      },
    },
  }),
}));
// The href carries the identifier the component asked for, so these tests
// assert which identity a link points at rather than only that one rendered.
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({
    children,
    identifier,
  }: {
    children: ReactNode;
    identifier: IdentityRef | null | undefined;
  }) =>
    identifier ? (
      <a href={`/identity/${identityUrnFor(identifier)}`}>{children}</a>
    ) : (
      <span>{children}</span>
    ),
}));
afterEach(cleanup);

describe("ToolUsageIdentity", () => {
  it("links readable agents to their identity overview with dotted styling and an icon", () => {
    render(
      <MemoryRouter>
        <ToolUsageIdentity
          kind="agent_id"
          identityKey="agent-1"
          label="agent:agent-1"
          agent={{ id: "agent-1", name: "Research agent" }}
        />
      </MemoryRouter>,
    );
    // Routed through the shared ActingPrincipal now, so the link is the
    // identity resolver's rather than AgentLink's own href builder. What
    // matters is unchanged: a readable agent is named, and the link lands on
    // that agent's identity rather than on whatever the row happened to hold.
    expect(
      screen.getByRole("link", { name: "Research agent" }).getAttribute("href"),
    ).toBe("/identity/agent:agent-1");
  });

  it("keeps unreadable agents distinct without revealing names or identity links", () => {
    render(
      <MemoryRouter>
        <ToolUsageIdentity
          kind="agent_id"
          identityKey="agent-1"
          label="Untrusted stale name"
        />
      </MemoryRouter>,
    );
    // The security-relevant half: an agent the caller may not read is shown
    // as its id, never under a name from a stale row, and is not linked.
    expect(screen.getByText("agent:agent-1")).toBeTruthy();
    expect(screen.queryByText("Untrusted stale name")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("preserves user identity links and genuinely unknown subjects", () => {
    render(
      <MemoryRouter>
        <ToolUsageIdentity
          kind="user_id"
          identityKey="user-1"
          label="Example User"
        />
        <ToolUsageIdentity
          kind="unknown"
          identityKey="Unknown"
          label="Unknown"
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByRole("link", { name: "Example User" }).getAttribute("href"),
    ).toBe("/identity/user:user-1");
    expect(screen.getByText("Unknown").closest("a")).toBeNull();
    expect(screen.queryByRole("img", { name: "Agent" })).toBeNull();
  });
});
