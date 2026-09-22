import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { ToolUsageIdentity } from "./tool-usage-identity";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    agents: { href: () => "/example/project/agent-management" },
  }),
}));
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({
    children,
    identifier,
  }: {
    children: ReactNode;
    identifier: unknown;
  }) =>
    identifier ? <a href="/identity">{children}</a> : <span>{children}</span>,
}));
afterEach(cleanup);

describe("ToolUsageIdentity", () => {
  it("links readable agents to agent settings with dotted styling and an icon", () => {
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
    const link = screen.getByRole("link", { name: "Research agent" });
    expect(link.getAttribute("href")).toBe(
      "/example/project/agent-management?id=agent-1",
    );
    expect(link.className).toContain("decoration-dotted");
    expect(screen.getByRole("img", { name: "Agent" })).toBeTruthy();
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
    expect(screen.getByText("agent:agent-1")).toBeTruthy();
    expect(screen.queryByText("Untrusted stale name")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByRole("img", { name: "Agent" })).toBeTruthy();
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
    ).toBe("/identity");
    expect(screen.getByText("Unknown").closest("a")).toBeNull();
    expect(screen.queryByRole("img", { name: "Agent" })).toBeNull();
  });
});
