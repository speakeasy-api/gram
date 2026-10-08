import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentLink } from "./agent-link";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: {
      detail: {
        overview: {
          href: (urn: string) =>
            `/org/projects/project/identities/${urn}/overview`,
        },
      },
    },
  }),
}));

afterEach(cleanup);

describe("AgentLink", () => {
  // An agent has one page, under Identities. It used to have a second one at
  // agent-management, and which you reached depended on where you clicked.
  it("opens the authorized agent's identity page", () => {
    const onRowClick = vi.fn<() => void>();
    render(
      <MemoryRouter>
        <div onClick={onRowClick}>
          <AgentLink
            agentId="agent/with?reserved&characters"
            className="custom"
          >
            Release assistant
          </AgentLink>
        </div>
      </MemoryRouter>,
    );

    const link = screen.getByRole("link", { name: "Release assistant" });
    expect(link.getAttribute("href")).toBe(
      "/org/projects/project/identities/agent%3Aagent%2Fwith%3Freserved%26characters/overview",
    );
    expect(link.classList.contains("custom")).toBe(true);
    fireEvent.click(link);
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("renders plain text when no authorized agent ID is available", () => {
    render(
      <MemoryRouter>
        <AgentLink className="custom">Agent</AgentLink>
      </MemoryRouter>,
    );

    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Agent").classList.contains("custom")).toBe(true);
  });
});
