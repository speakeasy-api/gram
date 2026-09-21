import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentLink } from "./agent-link";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    agents: { href: () => "/org/projects/project/agent-management" },
  }),
}));

afterEach(cleanup);

describe("AgentLink", () => {
  it("selects the authorized agent at the project management destination", () => {
    const onRowClick = vi.fn();
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
      "/org/projects/project/agent-management?id=agent%2Fwith%3Freserved%26characters",
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
