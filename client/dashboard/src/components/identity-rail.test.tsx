import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it } from "vitest";

import { IdentityRail } from "./identity-rail";

afterEach(cleanup);

function rail(badge?: number) {
  render(
    <MemoryRouter>
      <IdentityRail
        items={[
          {
            key: "mcp-sign-in",
            title: "MCP sign-in",
            href: "/mcp-sign-in",
            active: false,
            badge,
          },
        ]}
      />
    </MemoryRouter>,
  );
}

it("draws a count on a tab that needs attention, read with its title", () => {
  rail(2);
  expect(screen.getByText("2")).toBeTruthy();
  expect(screen.getByLabelText(", 2 need attention")).toBeTruthy();
});

it("draws nothing for a tab with nothing to do", () => {
  rail(0);
  expect(screen.getByRole("link").textContent).toBe("MCP sign-in");
});
